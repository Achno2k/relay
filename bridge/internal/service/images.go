package service

import (
	"context"
	"os"
	"strconv"
	"sync"

	"relay/internal/api"
	"relay/internal/transcript"
)

// imageCacheBytes bounds the decoded tool images kept for GET …/tool-images.
const imageCacheBytes = 32 << 20

// imageCache is a small LRU of decoded tool images. A tool result never changes once written,
// so entries never go stale.
type imageCache struct {
	mu      sync.Mutex
	entries []imageEntry // most recent first
	size    int
}

type imageEntry struct {
	key string
	img transcript.ToolImage
}

func (c *imageCache) get(key string) (transcript.ToolImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.entries {
		if e.key == key {
			copy(c.entries[1:i+1], c.entries[:i])
			c.entries[0] = e
			return e.img, true
		}
	}
	return transcript.ToolImage{}, false
}

func (c *imageCache) put(key string, img transcript.ToolImage) {
	if len(img.Data) > imageCacheBytes/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append([]imageEntry{{key, img}}, c.entries...)
	c.size += len(img.Data)
	for c.size > imageCacheBytes {
		last := c.entries[len(c.entries)-1]
		c.entries = c.entries[:len(c.entries)-1]
		c.size -= len(last.img.Data)
	}
}

// ToolImage serves one image of a tool result from the agent's transcript; see api.md
// "Tool result images".
func (s *Service) ToolImage(ctx context.Context, id, toolCallID string, index int) (transcript.ToolImage, error) {
	sn, err := s.SnapshotOf(ctx, id)
	if err != nil {
		return transcript.ToolImage{}, err
	}
	if sn.Agent.TranscriptState != api.TranscriptReady || sn.Transcript == nil {
		return transcript.ToolImage{}, api.NotFound("no transcript")
	}
	ref := *sn.Transcript
	key := ref.Path + "\x00" + toolCallID + "\x00" + strconv.Itoa(index)
	if img, ok := s.images.get(key); ok {
		return img, nil
	}
	f, err := os.Open(ref.Path)
	if err != nil {
		return transcript.ToolImage{}, api.NotFound("no transcript")
	}
	defer f.Close()
	img, err := transcript.FindToolImage(f, ref.Format, toolCallID, index)
	if err != nil {
		return transcript.ToolImage{}, err
	}
	s.images.put(key, img)
	return img, nil
}
