package transcript

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"strings"
	"testing"

	"relay/internal/api"
)

// The 3×2 synthetic PNG from docs/fixtures/tool-image.png.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../docs/fixtures/tool-image.png")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func jsonLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func claudeImageLines(t *testing.T, png []byte) string {
	return jsonLine(t, map[string]any{
		"type": "assistant", "uuid": "a1", "timestamp": "2026-10-02T09:00:00Z",
		"message": map[string]any{"content": []any{map[string]any{
			"type": "tool_use", "id": "toolu_img", "name": "Read", "input": map[string]any{"file_path": "/Users/dev/shop-api/shot.png"}}}},
	}) + "\n" + jsonLine(t, map[string]any{
		"type": "user", "uuid": "r1", "timestamp": "2026-10-02T09:00:01Z",
		"message": map[string]any{"content": []any{map[string]any{
			"type": "tool_result", "tool_use_id": "toolu_img", "content": []any{
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": b64(png)}},
			}}}},
	}) + "\n"
}

func TestToolImages_claudeReadListsTheImageAndKeepsThePreview(t *testing.T) {
	png := tinyPNG(t)
	msgs := Parse([]byte(claudeImageLines(t, png)), FormatClaude, "/Users/dev/shop-api", nil)
	if len(msgs) != 1 || len(msgs[0].Blocks) != 2 {
		t.Fatalf("%+v", msgs)
	}
	r := msgs[0].Blocks[1]
	expectEqual(t, r.Preview, "[image]")
	expectEqual(t, r.Images, []api.ToolImage{{Index: 0, MediaType: "image/png", Bytes: int64(len(png)), Width: 3, Height: 2}})
	out, _ := json.Marshal(r)
	if !strings.Contains(string(out), `"images":[{"index":0,"mediaType":"image/png","bytes":73,"width":3,"height":2}]`) {
		t.Errorf("%s", out)
	}
	// A text-only result has no images key at all.
	out, _ = json.Marshal(api.ToolResultBlock("t", false, "ok"))
	if strings.Contains(string(out), "images") {
		t.Errorf("%s", out)
	}
}

func TestToolImages_piAndCodexMCPImages(t *testing.T) {
	png := tinyPNG(t)
	pi := jsonLine(t, map[string]any{"type": "message", "id": "a1", "message": map[string]any{"role": "assistant",
		"content": []any{map[string]any{"type": "toolCall", "id": "call_1|fc_1", "name": "read", "arguments": map[string]any{"path": "a.png"}}}}}) + "\n" +
		jsonLine(t, map[string]any{"type": "message", "id": "r1", "message": map[string]any{"role": "toolResult", "toolCallId": "call_1|fc_1",
			"content": []any{
				map[string]any{"type": "text", "text": "Read image file [image/png]"},
				map[string]any{"type": "image", "data": b64(png), "mimeType": "image/png"},
				// No usable mimeType: sniffed from the bytes.
				map[string]any{"type": "image", "data": b64(png)},
				// Not an image: skipped, and doesn't take an index.
				map[string]any{"type": "image", "data": b64([]byte("plain text, not an image")), "mimeType": "text/plain"},
			}}}) + "\n"
	msgs := Parse([]byte(pi), FormatPi, "/Users/dev/website", nil)
	want := api.ToolImage{MediaType: "image/png", Bytes: 73, Width: 3, Height: 2}
	second := want
	second.Index = 1
	expectEqual(t, msgs[0].Blocks[1].Images, []api.ToolImage{want, second})

	codex := jsonLine(t, map[string]any{"timestamp": "2026-10-02T09:00:00Z", "type": "event_msg", "payload": map[string]any{
		"type": "item_completed", "item": map[string]any{"type": "McpToolCall", "id": "mcp1", "server": "browser", "tool": "screenshot",
			"arguments": map[string]any{}, "status": "completed",
			"result": map[string]any{"content": []any{map[string]any{"type": "image", "data": b64(png), "mimeType": "image/png"}}, "isError": false}}}}) + "\n"
	msgs = Parse([]byte(codex), FormatCodex, "/Users/dev/project", nil)
	expectEqual(t, msgs[0].Blocks[1].Images, []api.ToolImage{want})
}

func TestFindToolImage_decodesTheRightImageForEachKind(t *testing.T) {
	png := tinyPNG(t)
	// Lines before and after, so the scan has to skip unrelated ones.
	noise := jsonLine(t, map[string]any{"type": "user", "uuid": "u0", "message": map[string]any{"content": "hello toolu_img"}}) + "\n"
	transcript := noise + claudeImageLines(t, png) + noise
	got, err := FindToolImage(strings.NewReader(transcript), FormatClaude, "toolu_img", 0)
	if err != nil {
		t.Fatal(err)
	}
	expectEqual(t, got.MediaType, "image/png")
	if !bytes.Equal(got.Data, png) {
		t.Error("bytes differ")
	}

	for _, c := range []struct {
		id    string
		index int
	}{{"toolu_img", 1}, {"toolu_nope", 0}, {"", 0}, {"toolu_img", -1}} {
		_, err := FindToolImage(strings.NewReader(transcript), FormatClaude, c.id, c.index)
		var e *api.Error
		if !errors.As(err, &e) || e.Status != http.StatusNotFound {
			t.Errorf("%s/%d: %v", c.id, c.index, err)
		}
	}

	// pi reuses short ids: an earlier result with the same id and no image is skipped.
	earlier := jsonLine(t, map[string]any{"type": "message", "id": "r0", "message": map[string]any{"role": "toolResult", "toolCallId": "call_1",
		"content": []any{map[string]any{"type": "text", "text": "ok"}}}}) + "\n"
	pi := earlier + jsonLine(t, map[string]any{"type": "message", "id": "r1", "message": map[string]any{"role": "toolResult", "toolCallId": "call_1",
		"content": []any{map[string]any{"type": "image", "data": b64([]byte("x")), "mimeType": "image/gif"},
			map[string]any{"type": "image", "data": strings.TrimRight(b64(png), "="), "mimeType": "image/png"}}}}) + "\n"
	got, err = FindToolImage(strings.NewReader(pi), FormatPi, "call_1", 1)
	if err != nil || !bytes.Equal(got.Data, png) {
		t.Errorf("pi: %v", err)
	}

	codex := jsonLine(t, map[string]any{"type": "event_msg", "payload": map[string]any{"type": "item_completed",
		"item": map[string]any{"type": "McpToolCall", "id": "mcp1",
			"result": map[string]any{"content": []any{map[string]any{"type": "image", "data": b64(png), "mimeType": "image/png"}}}}}})
	// No trailing newline: the last line still counts.
	got, err = FindToolImage(strings.NewReader(codex), FormatCodex, "mcp1", 0)
	if err != nil || !bytes.Equal(got.Data, png) {
		t.Errorf("codex: %v", err)
	}
}

func TestFindToolImage_tooLargeAndBadBase64(t *testing.T) {
	png := tinyPNG(t)
	old := maxImageBytes
	maxImageBytes = 50
	defer func() { maxImageBytes = old }()
	_, err := FindToolImage(strings.NewReader(claudeImageLines(t, png)), FormatClaude, "toolu_img", 0)
	var e *api.Error
	if !errors.As(err, &e) || e.Status != http.StatusRequestEntityTooLarge || e.Code != "too_large" {
		t.Errorf("%v", err)
	}
	maxImageBytes = old

	bad := strings.Replace(claudeImageLines(t, png), b64(png), "!!!!not-base64!!!!", 1)
	if _, err := FindToolImage(strings.NewReader(bad), FormatClaude, "toolu_img", 0); !errors.As(err, &e) || e.Status != http.StatusNotFound {
		t.Errorf("%v", err)
	}
}

func TestDecodedLen(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 5, 100, 1001} {
		raw := bytes.Repeat([]byte{0xab}, n)
		padded := b64(raw)
		wrapped := ""
		for i := 0; i < len(padded); i += 76 {
			wrapped += padded[i:min(i+76, len(padded))] + "\r\n"
		}
		for _, s := range []string{padded, strings.TrimRight(padded, "="), wrapped} {
			if got := decodedLen(s); got != int64(n) {
				t.Errorf("%d: %q gave %d", n, s, got)
			}
			if n > 0 {
				if d, err := decodeImage(s); err != nil || len(d) != n {
					t.Errorf("%d: decode %v", n, err)
				}
			}
		}
	}
}

func TestImageSize_headersOnly(t *testing.T) {
	// JPEG via the standard decoder.
	img := image.NewRGBA(image.Rect(0, 0, 40, 25))
	img.Set(1, 1, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	w, h := imageSize(decodeHead(b64(buf.Bytes())))
	expectEqual(t, [2]int{w, h}, [2]int{40, 25})

	// WebP headers, hand-made: VP8X (canvas), VP8L (lossless), VP8 (lossy).
	riff := func(chunk string, body []byte) []byte {
		b := append([]byte("RIFF\x00\x00\x00\x00WEBP"+chunk+"\x00\x00\x00\x00"), body...)
		return append(b, make([]byte, 16)...)
	}
	vp8x := riff("VP8X", []byte{0, 0, 0, 0, 0x1f, 0x03, 0x00, 0xc7, 0x00, 0x00})   // 800×200
	vp8l := riff("VP8L", []byte{0x2f, 0x63, 0xc0, 0x31, 0x00})                     // 100×200: (100-1) | (200-1)<<14
	vp8 := riff("VP8 ", []byte{0, 0, 0, 0x9d, 0x01, 0x2a, 0x40, 0x01, 0xf0, 0x00}) // 320×240
	for _, c := range []struct {
		b    []byte
		w, h int
	}{{vp8x, 800, 200}, {vp8l, 100, 200}, {vp8, 320, 240}} {
		w, h := imageSize(c.b)
		expectEqual(t, [2]int{w, h}, [2]int{c.w, c.h})
	}

	// Truncated or unknown: no size, no panic.
	for _, b := range [][]byte{nil, []byte("RIFF"), tinyPNG(t)[:10], []byte("GIF89a")} {
		if w, h := imageSize(b); w != 0 || h != 0 {
			t.Errorf("%q: %d×%d", b, w, h)
		}
	}
}
