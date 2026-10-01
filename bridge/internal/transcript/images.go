package transcript

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif" // DecodeConfig only: header sizes
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"strings"

	"relay/internal/api"
)

// MaxImageBytes caps one served tool image (api.md "Tool result images").
const MaxImageBytes = 20 << 20

var maxImageBytes int64 = MaxImageBytes // tests lower it

// headBytes is how much of an image is decoded to read its size: enough for a JPEG's SOF after
// EXIF, never the whole image.
const headBytes = 64 << 10

var imageMediaType = regexp.MustCompile(`^image/[a-z0-9.+-]+$`)

// imageItem is one image in a tool result's content, still base64.
type imageItem struct {
	mediaType string
	data      string
}

// imageItems is the images in a tool result's content, in order: Claude's
// `{"type":"image","source":{"type":"base64","media_type","data"}}`, and pi's and MCP's
// `{"type":"image","data","mimeType"}`. Only image/* media types count.
func imageItems(content any) []imageItem {
	arr, ok := objects(content)
	if !ok {
		return nil
	}
	var out []imageItem
	for _, b := range arr {
		if strOr(b["type"], "") != "image" {
			continue
		}
		var mt, data string
		if src, ok := obj(b["source"]); ok {
			if strOr(src["type"], "") != "base64" {
				continue
			}
			mt, data = strOr(src["media_type"], ""), strOr(src["data"], "")
		} else {
			mt, data = strOr(b["mimeType"], ""), strOr(b["data"], "")
		}
		if data == "" {
			continue
		}
		mt = strings.ToLower(strings.TrimSpace(mt))
		if !imageMediaType.MatchString(mt) {
			mt = http.DetectContentType(decodeHead(data))
		}
		if !strings.HasPrefix(mt, "image/") {
			continue
		}
		out = append(out, imageItem{mediaType: mt, data: data})
	}
	return out
}

// toolImages lists a tool result's images for its toolResult block.
func toolImages(content any) []api.ToolImage {
	items := imageItems(content)
	if len(items) == 0 {
		return nil
	}
	out := make([]api.ToolImage, len(items))
	for i, it := range items {
		w, h := imageSize(decodeHead(it.data))
		out[i] = api.ToolImage{Index: i, MediaType: it.mediaType, Bytes: decodedLen(it.data), Width: w, Height: h}
	}
	return out
}

// decodedLen is the size base64 data decodes to, without decoding it.
func decodedLen(data string) int64 {
	var n int64 // base64 characters, not counting padding or line breaks
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '=', '\r', '\n':
		default:
			n++
		}
	}
	return n * 6 / 8
}

// decodeHead decodes up to headBytes from the start of base64 data.
func decodeHead(data string) []byte {
	chars := min(len(data), headBytes/3*4+64)
	s := data[:chars]
	if strings.ContainsAny(s, "\r\n") {
		s = strings.NewReplacer("\r", "", "\n", "").Replace(s)
	}
	s = strings.TrimRight(s, "=")
	s = s[:len(s)/4*4]
	dst := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
	n, _ := base64.StdEncoding.Decode(dst, []byte(s))
	return dst[:n]
}

// decodeImage decodes a whole image's base64 (padded or not).
func decodeImage(data string) ([]byte, error) {
	if strings.ContainsAny(data, "\r\n") {
		data = strings.NewReplacer("\r", "", "\n", "").Replace(data)
	}
	if b, err := base64.StdEncoding.DecodeString(data); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(data, "="))
}

// imageSize reads pixel dimensions from an image's first bytes: png, jpeg and gif through the
// standard decoders' DecodeConfig, webp by hand. 0, 0 when unknown.
func imageSize(head []byte) (int, int) {
	if w, h, ok := webpSize(head); ok {
		return w, h
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func webpSize(b []byte) (int, int, bool) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	u24 := func(p []byte) int { return int(p[0]) | int(p[1])<<8 | int(p[2])<<16 }
	switch string(b[12:16]) {
	case "VP8X":
		return u24(b[24:27]) + 1, u24(b[27:30]) + 1, true
	case "VP8L":
		if b[20] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(b[21:25])
		return int(bits&0x3fff) + 1, int(bits>>14&0x3fff) + 1, true
	case "VP8 ":
		if b[23] != 0x9d || b[24] != 0x01 || b[25] != 0x2a {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff), true
	}
	return 0, 0, false
}

// ToolImage is one decoded tool result image.
type ToolImage struct {
	MediaType string
	Data      []byte
}

var errNoImage = api.NotFound("no such tool image")

// FindToolImage scans a whole transcript, one line at a time, for the first tool result of
// toolCallID that has an index-th image, and decodes it. Memory stays at one line plus the
// image. Errors are *api.Error: 404 not_found (no result, no image at index, bad base64),
// 413 too_large.
func FindToolImage(r io.Reader, format Format, toolCallID string, index int) (ToolImage, error) {
	if toolCallID == "" || index < 0 {
		return ToolImage{}, errNoImage
	}
	needle := []byte(toolCallID)
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && bytes.Contains(line, needle) {
			// pi reuses short ids: a result without this image isn't the one.
			if content, ok := toolResultContent(line, format, toolCallID); ok {
				if items := imageItems(content); index < len(items) {
					return decodeItem(items[index])
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ToolImage{}, errNoImage
			}
			return ToolImage{}, err
		}
	}
}

func decodeItem(it imageItem) (ToolImage, error) {
	if decodedLen(it.data) > maxImageBytes {
		return ToolImage{}, api.NewError(http.StatusRequestEntityTooLarge, "too_large", "images are limited to 20 MB")
	}
	data, err := decodeImage(it.data)
	if err != nil || len(data) == 0 {
		return ToolImage{}, errNoImage
	}
	return ToolImage{MediaType: it.mediaType, Data: data}, nil
}

// toolResultContent is the content of the tool result for toolCallID on this line, if it has one.
func toolResultContent(line []byte, format Format, toolCallID string) (any, bool) {
	o, ok := readJSONObject(bytes.TrimRight(line, "\r\n"))
	if !ok {
		return nil, false
	}
	switch format {
	case FormatClaude:
		message, _ := obj(o["message"])
		if strOr(o["type"], "") != "user" || message == nil {
			return nil, false
		}
		content, _ := objects(message["content"])
		for _, b := range content {
			if strOr(b["type"], "") == "tool_result" && strOr(b["tool_use_id"], "") == toolCallID {
				return b["content"], true
			}
		}
	case FormatPi:
		message, _ := obj(o["message"])
		if strOr(o["type"], "") == "message" && message != nil &&
			strOr(message["role"], "") == "toolResult" && strOr(message["toolCallId"], "") == toolCallID {
			return message["content"], true
		}
	case FormatCodex:
		payload, _ := obj(o["payload"])
		item, _ := obj(payload["item"])
		if strOr(o["type"], "") == "event_msg" && strOr(payload["type"], "") == "item_completed" && item != nil &&
			strOr(item["type"], "") == "McpToolCall" && strOr(item["id"], "") == toolCallID {
			result, _ := obj(item["result"])
			return result["content"], true
		}
	}
	return nil, false
}
