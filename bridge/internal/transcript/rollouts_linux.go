package transcript

import (
	"os"
	"time"

	"relay/internal/api"
)

// createdTime: Linux's stat has no birth time, so it's when codex says the session started
// (the session_meta line's timestamp), else the modification time.
func createdTime(info os.FileInfo, meta sessionMeta) (time.Time, bool) {
	if t, ok := api.ParseTimestamp(meta.timestamp); ok {
		return t, true
	}
	return info.ModTime(), true
}
