package handlers

import (
	"testing"

	"projectview/internal/storage"
)

// What a file *is* has to be decided from the bytes, because the two other
// sources are both attacker-controlled: the multipart Content-Type is whatever
// the client typed, and the extension is whatever the uploader named it.
func TestDetectContentType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + "the rest does not matter for sniffing")
	pdf := []byte("%PDF-1.7\nsome pdf body")

	cases := []struct {
		name     string
		declared string
		filename string
		content  []byte
		want     string
	}{
		{
			name:     "the bytes beat a lying client",
			declared: "text/plain",
			filename: "notes.txt",
			content:  png,
			want:     "image/png",
		},
		{
			name:     "the bytes beat a lying extension",
			declared: "application/pdf",
			filename: "invoice.pdf",
			content:  png,
			want:     "image/png",
		},
		{
			name:     "a real pdf is recognised",
			declared: "application/octet-stream",
			filename: "report.pdf",
			content:  pdf,
			want:     "application/pdf",
		},
		{
			// Every modern Office document is a ZIP as far as sniffing is
			// concerned, so the extension is what separates them.
			name:     "the extension resolves what sniffing cannot",
			declared: "application/octet-stream",
			filename: "budget.xlsx",
			content:  []byte("PK\x03\x04zip-ish payload"),
			want:     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
		{
			name:     "parameters are stripped",
			declared: "text/csv; charset=utf-8",
			filename: "rows.csv",
			content:  []byte("a,b,c\n1,2,3\n"),
			want:     "text/csv",
		},
		{
			// The client's claim is the last resort, not the first, and it is
			// only reached for something nothing else recognises.
			name:     "an unrecognised file falls back to what the client said",
			declared: "application/x-custom",
			filename: "thing.unknownext",
			content:  []byte{0x00, 0x01, 0x02, 0x03},
			want:     "application/x-custom",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectContentType(tc.declared, tc.filename, tc.content); got != tc.want {
				t.Errorf("detectContentType = %q, want %q", got, tc.want)
			}
		})
	}
}

// An uploaded HTML document is recorded honestly as HTML - hiding what it is
// would help nobody. What keeps it from becoming stored cross-site scripting is
// that it is never rendered in place: the signed URL carries an "attachment"
// disposition, so the browser saves it instead of executing it against this
// application's origin.
func TestUploadedMarkupIsNeverRenderedInPlace(t *testing.T) {
	markup := []byte("<html><script>alert(document.cookie)</script></html>")

	if got := detectContentType("text/html", "page.html", markup); got != "text/html" {
		t.Errorf("detectContentType = %q, want text/html", got)
	}
	for _, name := range []string{"page.html", "page.htm", "logo.svg", "data.xml"} {
		if storage.IsInlineRenderable(name) {
			t.Errorf("%s would be rendered inline, which makes it a stored XSS vector", name)
		}
	}
}

// The type table has to be the application's own, not the host's.
//
// This test exists because the bug it guards against is invisible on a
// developer's machine: Go's mime.TypeByExtension reads the operating system's
// MIME database, which Windows has and the Alpine runtime image does not. Every
// Office document was resolving correctly in development and would have arrived
// as application/octet-stream in production - which an allow-list naming those
// types would then have refused.
func TestOfficeTypesDoNotDependOnTheHost(t *testing.T) {
	zipBytes := []byte("PK\x03\x04 and then some archive payload")

	cases := map[string]string{
		"budget.xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"notes.docx":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"deck.pptx":   "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
	for filename, want := range cases {
		// Declared as octet-stream, which is what a browser sends when it does
		// not recognise the file either.
		if got := detectContentType("application/octet-stream", filename, zipBytes); got != want {
			t.Errorf("detectContentType(%q) = %q, want %q", filename, got, want)
		}
	}

	// And the table is consulted rather than the host's, on every platform.
	if got := storage.ExtensionType("rows.csv"); got != "text/csv" {
		t.Errorf("ExtensionType(rows.csv) = %q, want text/csv", got)
	}
	if got := storage.ExtensionType("photo.png"); got != "" {
		t.Errorf("ExtensionType(photo.png) = %q; formats the bytes identify need no entry", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		25 << 20:  "25 MB",
		250 << 20: "250 MB",
		1 << 30:   "1 GB",
		512:       "512 bytes",
		2 << 10:   "2 kB",
	}
	for input, want := range cases {
		if got := humanBytes(input); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", input, got, want)
		}
	}
}
