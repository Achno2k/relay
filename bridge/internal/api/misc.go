package api

// AttachmentKind: image | pdf | file.
type AttachmentKind string

const (
	AttachmentImage AttachmentKind = "image"
	AttachmentPDF   AttachmentKind = "pdf"
	AttachmentFile  AttachmentKind = "file"
)

type Attachment struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Kind AttachmentKind `json:"kind"`
	Size int            `json:"size"`
}

// Machine is GET /machine.
type Machine struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Model string `json:"model"`
	OS    string `json:"os"`
}

// Health is GET /health.
type Health struct {
	OK            bool   `json:"ok"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Herdr         string `json:"herdr"`
	UptimeSeconds int    `json:"uptimeSeconds"`
}

func NewHealth(herdr string, uptimeSeconds int) Health {
	return Health{OK: true, Name: "relay", Version: Version, Herdr: herdr, UptimeSeconds: uptimeSeconds}
}

// Empty is the `{}` body of 202 responses.
type Empty struct{}
