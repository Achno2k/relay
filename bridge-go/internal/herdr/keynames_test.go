package herdr

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func TestAcceptsKnownShapes(t *testing.T) {
	for _, k := range []string{"esc", "enter", "up", "down", "tab", "space", "f1", "f24", "1", "9", "12", "a", "Z", "?", "ctrl+u", "shift+tab", "alt+a"} {
		if !IsValidKey(k) {
			t.Errorf("expected %q to be valid", k)
		}
	}
}

func TestRejectsGarbage(t *testing.T) {
	for _, k := range []string{"", "esc; rm -rf /", "ctrl+", "+u", "ctrl+u+u", strings.Repeat("a", 40), "\n", "up\ndown", "ctrl+esc+u", "123"} {
		if IsValidKey(k) {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}

func TestFuzzNeverCrashes(t *testing.T) {
	pool := []rune("abcdefgxyz012+-esc\n\t🎉\x00\x7f")
	for range 2000 {
		n := rand.IntN(13)
		var b strings.Builder
		for range n {
			b.WriteRune(pool[rand.IntN(len(pool))])
		}
		_ = IsValidKey(b.String()) // must not panic or hang
	}
}

func TestFirstInvalidFindsTheBadOne(t *testing.T) {
	if k, bad := FirstInvalidKey([]string{"esc", "enter"}); bad {
		t.Errorf("got %q", k)
	}
	if k, bad := FirstInvalidKey([]string{"esc", "bogus!!"}); !bad || k != "bogus!!" {
		t.Errorf("got %q %v", k, bad)
	}
}
