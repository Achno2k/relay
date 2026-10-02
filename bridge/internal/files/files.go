// Package files serves GET /agents/:id/file: a file inside the agent's cwd, as text or image
// bytes. See api.md "Files".
package files

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"relay/internal/api"
)

const (
	// TextLimit caps FileContent.content; a larger file is cut and marked truncated.
	TextLimit = 1 << 20
	// ImageLimit caps an image answer.
	ImageLimit = 20 << 20
	// sniffLen is how much of the file decides text vs binary.
	sniffLen = 8 << 10
)

// Result is one of the two 200 shapes: Text (JSON) or Image (raw bytes with ContentType).
type Result struct {
	Text        *api.FileContent
	Image       []byte
	ContentType string
}

var (
	errForbidden   = api.NewError(http.StatusForbidden, "forbidden", "the path is outside the agent's folder")
	errUnsupported = api.NewError(http.StatusUnsupportedMediaType, "unsupported", "not a text file or an image")
	errTooLarge    = api.NewError(http.StatusRequestEntityTooLarge, "too_large", "images are limited to 20 MB")
)

// Read returns rel (relative to cwd) when it resolves inside cwd, symlinks followed.
func Read(cwd, rel string) (Result, error) {
	clean, real, err := Resolve(cwd, rel)
	if err != nil {
		return Result{}, err
	}
	if real == "" {
		return Result{}, api.NotFound("no such file")
	}
	fi, err := os.Stat(real)
	if err != nil {
		return Result{}, api.NotFound("no such file")
	}
	if fi.IsDir() {
		return Result{}, api.BadRequest("path is a directory")
	}
	if !fi.Mode().IsRegular() {
		return Result{}, errUnsupported
	}
	f, err := os.Open(real)
	if err != nil {
		return Result{}, api.NotFound("can't open the file")
	}
	defer f.Close()

	head := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if ct, ok := imageType(clean, head); ok {
		if fi.Size() > ImageLimit {
			return Result{}, errTooLarge
		}
		rest, err := io.ReadAll(io.LimitReader(f, ImageLimit))
		if err != nil {
			return Result{}, api.NotFound("can't read the file")
		}
		return Result{Image: append(head, rest...), ContentType: ct}, nil
	}
	if !isText(head, int64(n) < fi.Size()) {
		return Result{}, errUnsupported
	}
	rest, err := io.ReadAll(io.LimitReader(f, TextLimit+1-int64(n)))
	if err != nil {
		return Result{}, api.NotFound("can't read the file")
	}
	data := append(head, rest...)
	truncated := len(data) > TextLimit || fi.Size() > int64(len(data))
	if truncated {
		data = cut(data[:min(len(data), TextLimit)])
	}
	return Result{Text: &api.FileContent{
		Path:      filepath.ToSlash(clean),
		Content:   strings.ToValidUTF8(string(data), "�"),
		Size:      fi.Size(),
		Truncated: truncated,
		Language:  Language(clean),
	}}, nil
}

// Resolve applies the path rules of GET /file: 400 for an empty, absolute or NUL path, 404 when
// the cwd is unknown or gone, 403 when rel resolves outside the cwd (symlinks followed, existing
// or not). clean is rel cleaned; real is where it resolves, "" when there's no such file.
func Resolve(cwd, rel string) (clean, real string, err error) {
	if cwd == "" {
		return "", "", api.NotFound("the agent has no known folder")
	}
	if rel == "" || strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "~") {
		return "", "", api.BadRequest("path must be relative to the agent's folder")
	}
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", "", api.NotFound("the agent's folder is gone")
	}
	clean = filepath.Clean(rel)
	// Lexical check first, so `../x` is 403 whether or not it exists.
	if !inside(root, filepath.Join(root, clean)) {
		return "", "", errForbidden
	}
	real, err = filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		// A dangling symlink that points outside is still 403, not 404.
		if errors.Is(err, fs.ErrNotExist) {
			if target, ok := danglingTarget(root, clean); ok && !inside(root, target) {
				return "", "", errForbidden
			}
		}
		return clean, "", nil
	}
	if !inside(root, real) {
		return "", "", errForbidden
	}
	return clean, real, nil
}

// inside: p is root or below it (both already clean and absolute).
func inside(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}

// danglingTarget resolves the parent and reads a final-component symlink by hand.
func danglingTarget(root, clean string) (string, bool) {
	full := filepath.Join(root, clean)
	target, err := os.Readlink(full)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(target) {
		dir, err := filepath.EvalSymlinks(filepath.Dir(full))
		if err != nil {
			return "", false
		}
		target = filepath.Join(dir, target)
	}
	return filepath.Clean(target), true
}

// isText: no NUL and valid UTF-8, allowing an incomplete sequence at the cut when more follows.
func isText(head []byte, more bool) bool {
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	if more {
		for i := 0; i < utf8.UTFMax && len(head) > 0 && !utf8.Valid(head); i++ {
			head = head[:len(head)-1]
		}
	}
	return utf8.Valid(head)
}

// cut ends data at its last newline, or at least on a UTF-8 boundary.
func cut(data []byte) []byte {
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		return data[:i+1]
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return data
}

var imageExts = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".heic": "image/heic", ".bmp": "image/bmp", ".tif": "image/tiff", ".tiff": "image/tiff",
}

// imageType: an image extension whose magic bytes agree.
func imageType(name string, head []byte) (string, bool) {
	ct, ok := imageExts[strings.ToLower(filepath.Ext(name))]
	if !ok {
		return "", false
	}
	has := func(off int, magic string) bool {
		return len(head) >= off+len(magic) && string(head[off:off+len(magic)]) == magic
	}
	switch ct {
	case "image/png":
		ok = has(0, "\x89PNG\r\n\x1a\n")
	case "image/jpeg":
		ok = has(0, "\xff\xd8\xff")
	case "image/gif":
		ok = has(0, "GIF87a") || has(0, "GIF89a")
	case "image/webp":
		ok = has(0, "RIFF") && has(8, "WEBP")
	case "image/heic":
		ok = has(4, "ftyp")
	case "image/bmp":
		ok = has(0, "BM")
	case "image/tiff":
		ok = has(0, "II*\x00") || has(0, "MM\x00*")
	}
	return ct, ok
}

var languages = map[string]string{
	".swift": "swift", ".go": "go", ".ts": "ts", ".tsx": "tsx", ".js": "js", ".jsx": "jsx", ".mjs": "js",
	".cjs": "js", ".py": "python", ".rb": "ruby", ".rs": "rust", ".java": "java", ".kt": "kotlin",
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".hpp": "cpp", ".m": "objc", ".mm": "objc",
	".cs": "csharp", ".php": "php", ".sh": "sh", ".bash": "sh", ".zsh": "sh", ".fish": "fish",
	".md": "md", ".markdown": "md", ".json": "json", ".jsonl": "json", ".yaml": "yaml", ".yml": "yaml",
	".toml": "toml", ".xml": "xml", ".plist": "xml", ".html": "html", ".htm": "html", ".css": "css",
	".scss": "scss", ".sql": "sql", ".svg": "xml", ".lua": "lua", ".dart": "dart", ".ex": "elixir",
	".exs": "elixir", ".vue": "vue", ".svelte": "svelte", ".proto": "proto", ".graphql": "graphql",
	".tf": "hcl", ".ini": "ini", ".txt": "text", ".diff": "diff", ".patch": "diff", ".csv": "csv",
}

var languageNames = map[string]string{
	"Dockerfile": "dockerfile", "Makefile": "make", "GNUmakefile": "make", "Gemfile": "ruby",
	"Podfile": "ruby", "Rakefile": "ruby", "go.mod": "go", "Package.swift": "swift",
}

// Language is the highlighting hint for a file name, "" when unknown.
func Language(name string) string {
	base := filepath.Base(name)
	if l, ok := languageNames[base]; ok {
		return l
	}
	return languages[strings.ToLower(filepath.Ext(base))]
}
