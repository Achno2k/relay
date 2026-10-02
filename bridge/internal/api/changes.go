package api

// Changes is GET /agents/:id/changes; see api.md "Changes".
type Changes struct {
	Repo        bool            `json:"repo"`
	Branch      *string         `json:"branch"`
	Upstream    *string         `json:"upstream"`
	Ahead       *int            `json:"ahead"`
	Behind      *int            `json:"behind"`
	Files       []ChangedFile   `json:"files"`
	MoreFiles   bool            `json:"moreFiles"`
	Commits     []CommitSummary `json:"commits"`
	MoreCommits bool            `json:"moreCommits"`
}

// MarshalJSON: `{"repo":false}` alone outside a work tree; otherwise branch and upstream are
// null when unset and ahead/behind are left out without an upstream.
func (c Changes) MarshalJSON() ([]byte, error) {
	if !c.Repo {
		return []byte(`{"repo":false}`), nil
	}
	return Marshal(struct {
		Repo        bool            `json:"repo"`
		Branch      *string         `json:"branch"`
		Upstream    *string         `json:"upstream"`
		Ahead       *int            `json:"ahead,omitzero"`
		Behind      *int            `json:"behind,omitzero"`
		Files       []ChangedFile   `json:"files"`
		MoreFiles   bool            `json:"moreFiles"`
		Commits     []CommitSummary `json:"commits"`
		MoreCommits bool            `json:"moreCommits"`
	}{c.Repo, c.Branch, c.Upstream, c.Ahead, c.Behind, nonNil(c.Files), c.MoreFiles, nonNil(c.Commits), c.MoreCommits})
}

// ChangedFile is one uncommitted file, or one file of a commit (CommitFile).
type ChangedFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitzero"`
	Status    string `json:"status"` // modified | added | deleted | renamed | untracked | conflicted
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
}

type CommitSummary struct {
	SHA       string `json:"sha"`
	ShortSHA  string `json:"shortSha"`
	Subject   string `json:"subject"`
	Time      string `json:"time"`
	FileCount int    `json:"fileCount"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// FileDiffText is GET /agents/:id/changes/diff.
type FileDiffText struct {
	Path      string `json:"path"`
	Diff      string `json:"diff"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

// CommitDetail is GET /agents/:id/changes/commits/:sha.
type CommitDetail struct {
	SHA       string       `json:"sha"`
	ShortSHA  string       `json:"shortSha"`
	Subject   string       `json:"subject"`
	Body      string       `json:"body"`
	Time      string       `json:"time"`
	Files     []CommitFile `json:"files"`
	Truncated bool         `json:"truncated"`
}

func (d CommitDetail) MarshalJSON() ([]byte, error) {
	type alias CommitDetail
	d.Files = nonNil(d.Files)
	return Marshal(alias(d))
}

type CommitFile struct {
	ChangedFile
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}
