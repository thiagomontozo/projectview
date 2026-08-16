package storage

import (
	"path"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// Naming, typing and the rules about what may be stored.
//
// Two names exist for every attachment and they are deliberately not the same
// string. The *storage key* is generated, opaque and made only of characters
// that are safe in a URL path. The *filename* is whatever the person uploaded,
// kept verbatim in PostgreSQL and handed back through a signed
// Content-Disposition. Deriving the key from the filename instead would put
// user input in a URL that a signature is computed over, and every accent,
// space and slash becomes a way for the two to disagree.

// ObjectKey builds the storage key for one attachment. The task id is part of
// the prefix so the objects belonging to a piece of work are adjacent in the
// bucket, which is what makes an operator's life bearable when something has
// to be reconciled by hand.
func ObjectKey(taskID, attachmentID uuid.UUID, filename string) string {
	key := "tasks/" + taskID.String() + "/" + attachmentID.String()
	if ext := safeExtension(filename); ext != "" {
		key += ext
	}
	return key
}

// safeExtension keeps a short lowercase alphanumeric suffix and drops anything
// else. It exists so an object can be identified by eye in the bucket, not so
// anything can be inferred from it - the content type is a column, never a
// guess from the name.
func safeExtension(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	if len(ext) < 2 || len(ext) > 11 {
		return ""
	}
	for _, r := range ext[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ""
		}
	}
	return ext
}

// CleanFilename reduces an uploaded name to something safe to store and
// display. Directory components are stripped - browsers do not send them, but
// an API client can, and "../../etc/passwd" must not survive into a header -
// and control characters are removed because they are how a filename is made
// to render as something other than what it is.
func CleanFilename(filename string) string {
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = path.Base(filename)
	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, filename)
	filename = strings.TrimSpace(filename)
	// path.Base answers "." or "/" for input that was nothing but separators.
	if filename == "" || filename == "." || filename == ".." || filename == "/" {
		return "file"
	}
	if len([]rune(filename)) > 200 {
		runes := []rune(filename)
		filename = string(runes[:200])
	}
	return filename
}

// extensionTypes maps the extensions worth naming to their media type.
//
// Carried explicitly rather than left to mime.TypeByExtension, which reads the
// operating system's MIME database — /etc/mime.types on Linux, the registry on
// Windows. The runtime image is Alpine and has neither, so on the machine that
// actually serves traffic that lookup answers "" for almost everything, while
// on a developer's Windows box it answers correctly. A rule that decides what
// may be uploaded must not vary with which box it runs on, and a table that
// disagrees with production is worse than no table.
//
// Only the formats sniffing cannot resolve are listed. Anything the bytes
// identify on their own — PNG, JPEG, PDF, GIF — never reaches here.
var extensionTypes = map[string]string{
	// Every OOXML document is a ZIP archive on the wire, so the extension is
	// the only thing that separates a spreadsheet from a presentation.
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".doc":  "application/msword",
	".xls":  "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",

	// Text formats, which all sniff as text/plain.
	".csv":  "text/csv",
	".txt":  "text/plain",
	".log":  "text/plain",
	".md":   "text/markdown",
	".json": "application/json",
	".xml":  "application/xml",
	".yaml": "application/yaml",
	".yml":  "application/yaml",
	".sql":  "application/sql",

	".zip": "application/zip",
	".gz":  "application/gzip",
	".tar": "application/x-tar",
	".7z":  "application/x-7z-compressed",
	".rar": "application/vnd.rar",
	".svg": "image/svg+xml",
}

// ExtensionType reports the media type an extension implies, or "" when the
// extension says nothing useful.
func ExtensionType(filename string) string {
	return extensionTypes[strings.ToLower(path.Ext(filename))]
}

// inlineExtensions are the types a browser may render in place rather than
// download. Images and PDFs only, and deliberately not SVG: an SVG is a
// document that can carry script, so rendering one inline from the same origin
// as the application would hand an uploader a stored cross-site scripting
// vector. It uploads fine; it simply downloads instead of displaying.
var inlineExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".avif": true, ".bmp": true, ".pdf": true,
}

// IsInlineRenderable reports whether the browser should display the file
// rather than save it.
func IsInlineRenderable(filename string) bool {
	return inlineExtensions[strings.ToLower(path.Ext(filename))]
}

// blockedExtensions never reach the bucket.
//
// This is a short deny-list on top of the operator's optional allow-list, and
// it covers one specific thing: formats a browser or an operating system will
// execute on a double-click. An internal tool where somebody can attach a
// .exe to a task and a colleague can open it from the task is a malware
// delivery mechanism with a nice interface, whatever the virus scanner says.
var blockedExtensions = map[string]bool{
	".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".com": true,
	".scr": true, ".pif": true, ".cpl": true, ".jar": true, ".hta": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true,
	".wsh": true, ".ps1": true, ".psm1": true, ".reg": true, ".lnk": true,
	".dll": true, ".sys": true, ".apk": true, ".app": true, ".dmg": true,
}

// IsBlockedFilename reports whether the extension is one that is never
// accepted. Double extensions are covered because only the final one decides
// what runs: "invoice.pdf.exe" is an executable.
func IsBlockedFilename(filename string) bool {
	return blockedExtensions[strings.ToLower(path.Ext(filename))]
}

// ContentTypeAllowed applies the operator's allow-list.
//
// An empty list allows everything, which is the default: an internal tool
// whose users cannot attach a .docx because nobody enumerated it is a tool
// people stop using. Entries may be exact ("application/pdf") or a wildcard
// over a group ("image/*").
func ContentTypeAllowed(contentType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	// Parameters are not part of the decision: "text/plain; charset=utf-8" is
	// text/plain.
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	for _, entry := range allowed {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == contentType || entry == "*/*" {
			return true
		}
		if strings.HasSuffix(entry, "/*") &&
			strings.HasPrefix(contentType, strings.TrimSuffix(entry, "*")) {
			return true
		}
	}
	return false
}
