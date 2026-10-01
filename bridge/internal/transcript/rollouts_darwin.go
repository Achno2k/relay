package transcript

import (
	"os"
	"syscall"
	"time"
)

// createdTime is the file's birth time, as Swift's `creationDateKey`.
func createdTime(info os.FileInfo, _ sessionMeta) (time.Time, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(st.Birthtimespec.Unix()), true
}
