package machine

import (
	"regexp"
	"testing"
)

func TestDarwinShape(t *testing.T) {
	m := Current()
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(m.ID) {
		t.Errorf("id %q is not a lowercase hardware UUID", m.ID)
	}
	if !regexp.MustCompile(`^macOS \d+\.\d+(\.\d+)?$`).MatchString(m.OS) {
		t.Errorf("os %q", m.OS)
	}
}
