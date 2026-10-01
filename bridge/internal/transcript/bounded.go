package transcript

import (
	"bytes"
	"io"
	"os"
)

// MaxReadBytes caps how much of a session file the bridge reads. Real sessions are a few MB; a
// huge one (a long-lived session, or a runaway agent) would otherwise mean holding the whole file
// plus every parsed message in memory just to answer one page. Past the cap only the tail is read,
// trading very old history in that one session for bounded memory.
const MaxReadBytes int64 = 64 << 20

// ReadBounded reads the file, or only its last cap bytes when size is over cap, aligned to the
// next newline so the first line is never a fragment of a bigger one.
func ReadBounded(path string, size, cap int64) ([]byte, error) {
	if size <= cap {
		return os.ReadFile(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(size-cap, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
		data = data[nl+1:]
	}
	return data, nil
}
