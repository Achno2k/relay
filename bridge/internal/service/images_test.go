package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"relay/internal/api"
	"relay/internal/transcript"
)

// A Read of an image, appended to w1:p1's transcript: listed in /messages, served by tool-images.
func TestRoutes_ToolImagesFromTheTranscript(t *testing.T) {
	app := withApp(t, nil)
	png, err := os.ReadFile("../../../docs/fixtures/tool-image.png")
	if err != nil {
		t.Fatal(err)
	}
	line := func(v any) []byte { b, _ := json.Marshal(v); return append(b, '\n') }
	var extra []byte
	extra = append(extra, line(map[string]any{"type": "assistant", "uuid": "img-a", "timestamp": "2026-10-02T09:00:00Z",
		"message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "toolu_img/1", "name": "Read",
			"input": map[string]any{"file_path": "/private/tmp/scratch/shot.png"}}}}})...)
	extra = append(extra, line(map[string]any{"type": "user", "uuid": "img-r", "timestamp": "2026-10-02T09:00:01Z",
		"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_img/1",
			"content": []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png",
				"data": base64.StdEncoding.EncodeToString(png)}}}}}}})...)
	f, err := os.OpenFile(app.transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(extra); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r := app.get(t, "/agents/w1%3Ap1/messages?limit=1")
	p := decodeInto[api.MessagePage](t, r)
	blocks := p.Messages[len(p.Messages)-1].Blocks
	last := blocks[len(blocks)-1]
	if last.ToolCallID != "toolu_img/1" || len(last.Images) != 1 ||
		last.Images[0] != (api.ToolImage{MediaType: "image/png", Bytes: 73, Width: 3, Height: 2}) {
		t.Fatalf("%+v", last)
	}
	if bytes.Contains(r.body, []byte("/private/")) {
		t.Errorf("leak: %s", r.body)
	}

	uri := "/agents/w1%3Ap1/tool-images/toolu_img%2F1/0"
	for range 2 { // second time from the cache
		img := app.get(t, uri)
		if img.status != http.StatusOK || !bytes.Equal(img.body, png) {
			t.Fatalf("%d %q", img.status, img.body)
		}
		if ct, cc := img.header.Get("Content-Type"), img.header.Get("Cache-Control"); ct != "image/png" || cc != "private, max-age=86400" {
			t.Errorf("%s / %s", ct, cc)
		}
	}

	for uri, want := range map[string]int{
		"/agents/w1%3Ap1/tool-images/toolu_img%2F1/1":  http.StatusNotFound,   // no such index
		"/agents/w1%3Ap1/tool-images/toolu_other/0":    http.StatusNotFound,   // no such result
		"/agents/w1%3Ap2/tool-images/toolu_img%2F1/0":  http.StatusNotFound,   // no transcript yet
		"/agents/w1%3Ap1/tool-images/toolu_img%2F1/-1": http.StatusBadRequest, // bad index
		"/agents/w1%3Ap1/tool-images/toolu_img%2F1/x":  http.StatusBadRequest,
		"/agents/w1%3Ap1/tool-images/toolu%01/0":       http.StatusBadRequest, // control character
		"/agents/w9%3Ap9/tool-images/toolu_img%2F1/0":  http.StatusNotFound,   // no agent
	} {
		got := app.get(t, uri)
		if got.status != want {
			t.Errorf("%s: %d %s", uri, got.status, got.body)
		}
		if strings.Contains(string(got.body), "/private/") {
			t.Errorf("%s leaked", uri)
		}
	}
	// Without the token: 401 like everything else.
	if r := app.do(t, "GET", uri, nil, nil); r.status != http.StatusUnauthorized {
		t.Errorf("%d", r.status)
	}
}

func TestImageCache_evictsLeastRecentlyUsedOverBudget(t *testing.T) {
	var c imageCache
	img := func(n int) transcript.ToolImage {
		return transcript.ToolImage{MediaType: "image/png", Data: make([]byte, n)}
	}
	quarter := imageCacheBytes / 4
	c.put("a", img(quarter))
	c.put("b", img(quarter))
	c.put("c", img(quarter))
	c.put("d", img(quarter))
	if _, ok := c.get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	c.put("e", img(1)) // over budget: b goes
	if _, ok := c.get("b"); ok {
		t.Error("b kept")
	}
	for _, k := range []string{"a", "c", "d", "e"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("%s evicted", k)
		}
	}
	c.put("huge", img(quarter+1)) // never cached
	if _, ok := c.get("huge"); ok {
		t.Error("huge cached")
	}
	if c.size > imageCacheBytes {
		t.Errorf("size %d", c.size)
	}
}
