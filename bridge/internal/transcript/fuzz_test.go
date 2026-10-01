package transcript

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// Ported from FuzzTests.swift (the scrubber and transcript cases; approval, picker, input box and
// upload names are ported by their owners). The property is "never crashes, never hangs, never
// leaks an absolute path".

var fuzzPool = []rune("abcXYZ012 \n\t\r❯›▶☐☒✔☑✓←→│┃║╭╮╰╯┌┐└┘─━═-–—╌┄()[]{}.,:;!?\"'`~@#$%^&*+=|\\/<>_🎉🙂👍️\u0000\u001B\u007F\u200B\uFEFF😀")

func randomString(rng *rand.Rand, maxLen int) string {
	n := rng.IntN(maxLen + 1)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(fuzzPool[rng.IntN(len(fuzzPool))])
	}
	return b.String()
}

func TestFuzz_pathScrubberNeverCrashesAndRemovesTheCwdAtAWordBoundary(t *testing.T) {
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	cwd := "/Users/dev/shop-api"
	scrubber := NewScrubber(cwd)
	// Boundary characters only, so cwd always ends a path component rather than starting a
	// longer sibling directory name (e.g. `/Users/dev/shop-apiXYZ`, which correctly isn't scrubbed).
	boundary := []rune(" \n\t/'\"()[]{}:;,|<>")
	for i := 0; i < 500; i++ {
		before := randomString(rng, 40)
		after := ""
		if rng.IntN(2) == 1 {
			after = string(boundary[rng.IntN(len(boundary))]) + randomString(rng, 40)
		}
		in := before + cwd + after
		if out := scrubber.Scrub(in); strings.Contains(out, cwd) {
			t.Fatalf("Scrub(%q) = %q still contains the cwd", in, out)
		}
	}
}

// The transcript parser must never crash on garbage JSONL, invalid UTF-8, or partial lines.
func TestFuzz_transcriptParserNeverCrashesOnGarbageLines(t *testing.T) {
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for _, format := range []Format{FormatClaude, FormatPi, FormatCodex} {
		p := NewParser(format, "/Users/dev/app", nil)
		for i := 0; i < 300; i++ {
			line := make([]byte, rng.IntN(501))
			for j := range line {
				line[j] = byte(rng.IntN(256))
			}
			p.ConsumeLine(line)
		}
	}
}

// Go-native fuzz targets (`go test -fuzz=FuzzScrub`); the seed corpus runs as a normal test.
func FuzzScrub(f *testing.F) {
	for _, s := range []string{"/Users/dev/shop-api/src/a.py", "~/.x/y", "a/b /c/d", "é/tmp/a/b", "https://x.y/a/b"} {
		f.Add(s)
	}
	s := NewScrubber("/Users/dev/shop-api")
	f.Fuzz(func(t *testing.T, in string) {
		s.Scrub(in)
		Truncate(in, 10)
		FirstLine(in)
	})
}

func FuzzParseLine(f *testing.F) {
	f.Add([]byte(`{"type":"user","uuid":"u","message":{"content":"hi"}}`))
	f.Add([]byte(`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["zsh","-lc","ls"]}}}`))
	f.Add([]byte(`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":1.5e300}}]}}`))
	f.Fuzz(func(t *testing.T, line []byte) {
		for _, format := range []Format{FormatClaude, FormatPi, FormatCodex} {
			NewParser(format, "/Users/dev/app", nil).ConsumeLine(line)
		}
	})
}
