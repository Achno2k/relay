package qr

import (
	"strings"
	"testing"
)

func TestTerminalShape(t *testing.T) {
	m, ok := Modules("relay://pair?url=http%3A%2F%2F100.64.0.1%3A7878&token=abc")
	if !ok || len(m) < 21 || len(m) != len(m[0]) {
		t.Fatalf("modules %d", len(m))
	}
	// Finder pattern in the top-left corner.
	if !m[0][0] || !m[0][6] || !m[6][0] || m[1][1] || !m[3][3] {
		t.Fatal("no finder pattern")
	}
	out, ok := Terminal("hello", 2)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	mm, _ := Modules("hello")
	if !ok || len(lines) != (len(mm)+4+1)/2 || !strings.HasPrefix(lines[0], "\x1b[30;107m") || !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Fatalf("%q", out)
	}
}
