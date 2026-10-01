package transcript

import (
	"regexp"
	"strings"
	"time"

	"relay/internal/api"
)

// Attached is what the bridge attached to a user message: the files, then the original text.
type Attached struct {
	Files []api.Attachment // Size is unused
	Text  string
}

// Uploads recognises the bridge's own attachments in user messages. *uploads.Store implements it.
type Uploads interface {
	// LookupSent finds the sent-log record for a message the bridge sent, even though Claude Code
	// rewrote its text. It answers false at once unless the text looks attached. A zero at means
	// the line's time is unknown.
	LookupSent(text string, at time.Time) (Attached, bool)
	// ParseMarker reads the `Attached files:` marker line with intact paths.
	ParseMarker(text string) (Attached, bool)
}

var pastedContentTag = regexp.MustCompile(`</?pasted_content[^>]*>\n?`)

// StripPastedContent removes the `<pasted_content …>` tags Claude Code wraps multi-line pastes in.
func StripPastedContent(text string) string {
	if !strings.Contains(text, "pasted_content") {
		return text
	}
	return strings.TrimSpace(pastedContentTag.ReplaceAllString(text, ""))
}
