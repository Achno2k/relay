package uploads

import (
	"path/filepath"
	"strings"

	"relay/internal/api"
)

// Extension tables taken from UTType on macOS 26 (what the Swift bridge used), so kind and
// Content-Type come out the same on Linux, where there's no UTType.

// imageExts conform to public.image.
var imageExts = map[string]bool{
	"jpg": true, "jpeg": true, "jpe": true, "png": true, "gif": true, "heic": true, "heif": true,
	"heics": true, "heifs": true, "webp": true, "tif": true, "tiff": true, "bmp": true, "ico": true,
	"icns": true, "svg": true, "svgz": true, "psd": true, "avif": true, "jxl": true, "jp2": true,
	"j2k": true, "jpf": true, "jpx": true, "dng": true, "cr2": true, "cr3": true, "crw": true,
	"nef": true, "nrw": true, "arw": true, "raf": true, "orf": true, "rw2": true, "pef": true,
	"srw": true, "raw": true, "exr": true, "tga": true, "hdr": true, "pbm": true, "pgm": true,
	"ppm": true, "xbm": true, "pict": true, "pct": true, "pntg": true, "sgi": true, "astc": true,
	"ktx": true, "dds": true, "jfx": true, "qti": true, "qtif": true, "ai": true, "3fr": true,
	"erf": true, "mos": true, "mrw": true, "dcr": true, "sr2": true, "srf": true, "rwl": true,
	"iiq": true, "fff": true, "dxo": true,
}

// mimeTypes is UTType.preferredMIMEType per extension; anything else is application/octet-stream.
var mimeTypes = map[string]string{
	"jpg": "image/jpeg", "jpeg": "image/jpeg", "jpe": "image/jpeg", "png": "image/png", "gif": "image/gif",
	"heic": "image/heic", "heif": "image/heif", "heics": "image/heic-sequence", "heifs": "image/heif-sequence",
	"webp": "image/webp", "tif": "image/tiff", "tiff": "image/tiff", "bmp": "image/bmp",
	"ico": "image/vnd.microsoft.icon", "svg": "image/svg+xml", "svgz": "image/svg+xml",
	"psd": "image/vnd.adobe.photoshop", "avif": "image/avif", "jxl": "image/jxl", "jp2": "image/jp2",
	"j2k": "image/jp2", "jpf": "image/jp2", "jpx": "image/jp2", "dng": "image/x-adobe-dng",
	"cr2": "image/x-canon-cr2", "cr3": "image/x-canon-cr3", "crw": "image/x-canon-crw",
	"nef": "image/x-nikon-nef", "nrw": "image/x-nikon-nrw", "arw": "image/x-sony-arw",
	"raf": "image/x-fuji-raf", "orf": "image/x-olympus-orf", "rw2": "image/x-panasonic-rw2",
	"pef": "image/x-pentax-pef", "srw": "image/x-samsung-srw", "raw": "image/x-panasonic-raw",
	"tga": "image/targa", "xbm": "image/x-xbitmap", "pict": "image/pict", "pct": "image/pict",
	"sgi": "image/sgi", "qti": "image/x-quicktime", "qtif": "image/x-quicktime",
	"3fr": "image/x-hasselblad-3fr", "erf": "image/x-epson-erf", "mos": "image/x-leaf-mos",
	"mrw": "image/x-minolta-mrw", "dcr": "image/x-kodak-dcr", "sr2": "image/x-sony-sr2",
	"srf": "image/x-sony-srf", "rwl": "image/x-leica-rwl", "iiq": "image/x-phaseone-iiq",
	"fff": "image/x-hasselblad-fff", "dxo": "image/x-dxo-dxo",
	"pdf": "application/pdf", "ps": "application/postscript",
	"txt": "text/plain", "md": "text/markdown", "markdown": "text/markdown", "json": "application/json",
	"py": "text/x-python-script", "rb": "text/x-ruby-script", "js": "text/javascript",
	"html": "text/html", "htm": "text/html", "css": "text/css", "csv": "text/csv", "rtf": "text/rtf",
	"xml": "application/xml", "yaml": "application/x-yaml", "yml": "application/x-yaml",
	"zip": "application/zip", "gz": "application/x-gzip", "tar": "application/x-tar",
	"bz2": "application/x-bzip2", "xz": "application/x-xz", "7z": "application/x-7z-compressed",
	"mp4": "video/mp4", "mov": "video/quicktime", "m4v": "video/x-m4v", "webm": "video/webm",
	"mp3": "audio/mpeg", "m4a": "audio/x-m4a", "wav": "audio/vnd.wave", "aac": "audio/aac",
	"flac": "audio/flac", "ogg": "audio/ogg",
	"doc": "application/msword", "xls": "application/vnd.ms-excel", "ppt": "application/vnd.ms-powerpoint",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"key":  "application/x-iwork-keynote-sffkey", "pages": "application/x-iwork-pages-sffpages",
	"numbers": "application/x-iwork-numbers-sffnumbers", "dmg": "application/x-apple-diskimage",
	"bin": "application/macbinary", "fig": "application/x-figma",
}

// ext is NSString.pathExtension, lowercased (UTType lookups ignore case).
func ext(name string) string {
	e := filepath.Ext(filepath.Base(name))
	return strings.ToLower(strings.TrimPrefix(e, "."))
}

// KindOf is the attachment kind from the file name's extension.
func KindOf(name string) api.AttachmentKind {
	e := ext(name)
	switch {
	case e == "pdf":
		return api.AttachmentPDF
	case imageExts[e]:
		return api.AttachmentImage
	}
	return api.AttachmentFile
}

// MimeType is the Content-Type for a stored upload.
func MimeType(path string) string {
	if m, ok := mimeTypes[ext(path)]; ok {
		return m
	}
	return "application/octet-stream"
}
