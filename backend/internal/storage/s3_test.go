package storage

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The signing tests below use Amazon's own published examples. That matters
// more than it might look: a signature is either byte-for-byte right or it is a
// 403 with no diagnosis, and a test written against our own output would pass
// just as happily on a wrong implementation. These are the vectors the other
// end verifies against.

const (
	exampleAccessKey = "AKIAIOSFODNN7EXAMPLE"
	exampleSecretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
)

var exampleTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

// AWS "Example: Signature calculation for a presigned URL" - GET test.txt from
// examplebucket, valid for 24 hours.
func TestPresignedGetMatchesAWSExample(t *testing.T) {
	s, err := New(Config{
		Endpoint:  "https://s3.amazonaws.com",
		Region:    "us-east-1",
		Bucket:    "examplebucket",
		AccessKey: exampleAccessKey,
		SecretKey: exampleSecretKey,
		// The published example addresses the bucket as a subdomain.
		ForcePathStyle: false,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// No filename and no content type: the example signs exactly five query
	// parameters, and adding a response-content-disposition would change the
	// canonical request the vector was computed over.
	signed, expiresAt, err := s.presignGetAt(exampleTime, "test.txt", "", "", 24*time.Hour)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}

	const want = "aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("the signed URL does not parse: %v", err)
	}
	if got := parsed.Query().Get("X-Amz-Signature"); got != want {
		t.Errorf("signature = %s, want %s", got, want)
	}
	if parsed.Host != "examplebucket.s3.amazonaws.com" {
		t.Errorf("host = %s, want examplebucket.s3.amazonaws.com", parsed.Host)
	}
	if parsed.Path != "/test.txt" {
		t.Errorf("path = %s, want /test.txt", parsed.Path)
	}
	if want := exampleTime.Add(24 * time.Hour); !expiresAt.Equal(want) {
		t.Errorf("expiry = %s, want %s", expiresAt, want)
	}
}

// AWS "Example: PUT Object" - the header-authorization form, which is what the
// backend uses for its own calls.
func TestHeaderAuthorizationMatchesAWSExample(t *testing.T) {
	headers := map[string]string{
		"date":                 "Fri, 24 May 2013 00:00:00 GMT",
		"host":                 "examplebucket.s3.amazonaws.com",
		"x-amz-content-sha256": "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072",
		"x-amz-date":           "20130524T000000Z",
		"x-amz-storage-class":  "REDUCED_REDUNDANCY",
	}

	canonical, signedHeaders := canonicalRequest(http.MethodPut, "/test%24file.text", "",
		headers, "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072")

	if want := "date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class"; signedHeaders != want {
		t.Errorf("signed headers = %s, want %s", signedHeaders, want)
	}

	got := signature(exampleSecretKey, exampleTime, "us-east-1",
		stringToSign(exampleTime, "us-east-1", canonical))
	const want = "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd"
	if got != want {
		t.Errorf("signature = %s, want %s", got, want)
	}
}

// A signature that does not change with the object it addresses would let one
// leaked link open every file in the bucket.
func TestSignatureIsBoundToTheKey(t *testing.T) {
	s, _ := New(Config{
		Endpoint: "http://minio:9000", Region: "us-east-1", Bucket: "attachments",
		AccessKey: exampleAccessKey, SecretKey: exampleSecretKey, ForcePathStyle: true,
	})

	first, _, err := s.presignGetAt(exampleTime, "attachments/a/one.pdf", "one.pdf", "application/pdf", time.Hour)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	second, _, err := s.presignGetAt(exampleTime, "attachments/a/two.pdf", "one.pdf", "application/pdf", time.Hour)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}

	sigOf := func(raw string) string {
		u, _ := url.Parse(raw)
		return u.Query().Get("X-Amz-Signature")
	}
	if sigOf(first) == sigOf(second) {
		t.Error("two different keys produced the same signature")
	}
}

// The public URL exists because the browser and the backend reach the store at
// different addresses, and the signature covers the host. Signing for the
// internal one would produce links that always fail verification.
func TestPresignUsesThePublicHost(t *testing.T) {
	s, _ := New(Config{
		Endpoint: "http://minio:9000", PublicURL: "https://projects.example.com",
		Region: "us-east-1", Bucket: "attachments",
		AccessKey: exampleAccessKey, SecretKey: exampleSecretKey, ForcePathStyle: true,
	})

	signed, _, err := s.presignGetAt(exampleTime, "attachments/a/b.pdf", "b.pdf", "application/pdf", time.Hour)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.HasPrefix(signed, "https://projects.example.com/attachments/attachments/a/b.pdf?") {
		t.Errorf("signed URL does not address the public host: %s", signed)
	}
}

func TestPathStyleAddressing(t *testing.T) {
	pathStyle, _ := New(Config{
		Endpoint: "http://minio:9000", Region: "us-east-1", Bucket: "files",
		AccessKey: exampleAccessKey, SecretKey: exampleSecretKey, ForcePathStyle: true,
	})
	if got := pathStyle.objectPath(pathStyle.endpoint, "a/b.txt"); got != "/files/a/b.txt" {
		t.Errorf("path style = %s, want /files/a/b.txt", got)
	}
	if got := pathStyle.host(pathStyle.endpoint); got != "minio:9000" {
		t.Errorf("path-style host = %s, want minio:9000", got)
	}

	virtualHost, _ := New(Config{
		Endpoint: "https://s3.eu-west-1.amazonaws.com", Region: "eu-west-1", Bucket: "files",
		AccessKey: exampleAccessKey, SecretKey: exampleSecretKey, ForcePathStyle: false,
	})
	if got := virtualHost.objectPath(virtualHost.endpoint, "a/b.txt"); got != "/a/b.txt" {
		t.Errorf("virtual-host path = %s, want /a/b.txt", got)
	}
	if got := virtualHost.host(virtualHost.endpoint); got != "files.s3.eu-west-1.amazonaws.com" {
		t.Errorf("virtual-host host = %s, want files.s3.eu-west-1.amazonaws.com", got)
	}
}

// An incomplete configuration yields a nil client rather than an error, and a
// nil client has to be callable: every attachment endpoint would otherwise need
// its own nil check before it could report the feature as unavailable.
func TestUnconfiguredStoreIsSafeToCall(t *testing.T) {
	s, err := New(Config{Endpoint: "http://minio:9000"}) // no bucket, no credentials
	if err != nil {
		t.Fatalf("an incomplete configuration is not an error: %v", err)
	}
	if s.Enabled() {
		t.Fatal("a store with no bucket reports itself as enabled")
	}
	if _, _, err := s.PresignGet("k", "f.pdf", "application/pdf", time.Hour); err != ErrDisabled {
		t.Errorf("PresignGet on a disabled store = %v, want ErrDisabled", err)
	}
	if err := s.Delete(t.Context(), "k"); err != ErrDisabled {
		t.Errorf("Delete on a disabled store = %v, want ErrDisabled", err)
	}
	if s.Bucket() != "" {
		t.Error("a disabled store named a bucket")
	}
}

func TestMalformedEndpointIsRejected(t *testing.T) {
	if _, err := New(Config{
		Endpoint: "minio:9000", Bucket: "b", AccessKey: "k", SecretKey: "s",
	}); err == nil {
		t.Error("an endpoint without a scheme was accepted")
	}
}

// ---------------------------------------------------------------------------
// Content-Disposition
// ---------------------------------------------------------------------------

func TestContentDisposition(t *testing.T) {
	// Images render in place; a spreadsheet downloads.
	if got := contentDisposition("screenshot.png"); !strings.HasPrefix(got, "inline;") {
		t.Errorf("png disposition = %s, want inline", got)
	}
	if got := contentDisposition("budget.xlsx"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("xlsx disposition = %s, want attachment", got)
	}
	// An SVG is a scriptable document, so it downloads rather than rendering
	// from the application's own origin.
	if got := contentDisposition("logo.svg"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("svg disposition = %s, want attachment", got)
	}

	// A name with accents needs the RFC 5987 form as well, or it arrives
	// mangled.
	got := contentDisposition("relatório final.pdf")
	if !strings.Contains(got, "filename*=UTF-8''relat%C3%B3rio%20final.pdf") {
		t.Errorf("accented filename lost its UTF-8 form: %s", got)
	}

	// A quote in the filename must not be able to close the quoted string and
	// append a directive of its own.
	got = contentDisposition(`in"voice.pdf`)
	if !strings.Contains(got, `filename="in\"voice.pdf"`) {
		t.Errorf("a quote in the filename was not escaped: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Keys, names and types
// ---------------------------------------------------------------------------

func TestObjectKeyIsGeneratedNotDerivedFromTheName(t *testing.T) {
	taskID, attachmentID := uuid.New(), uuid.New()

	key := ObjectKey(taskID, attachmentID, "relatório final.pdf")
	want := "tasks/" + taskID.String() + "/" + attachmentID.String() + ".pdf"
	if key != want {
		t.Errorf("key = %s, want %s", key, want)
	}

	// A name whose extension is not a plain suffix contributes nothing to the
	// key, rather than smuggling characters into a signed URL path.
	if got := ObjectKey(taskID, attachmentID, "report.tar.gz~"); strings.ContainsAny(got, "~ ") {
		t.Errorf("key picked up unsafe characters: %s", got)
	}
}

func TestCleanFilename(t *testing.T) {
	cases := map[string]string{
		`C:\Users\ana\report.pdf`: "report.pdf",
		"../../etc/passwd":        "passwd",
		"notes.txt":               "notes.txt",
		"relatório final.pdf":     "relatório final.pdf",
		"  spaced.png  ":          "spaced.png",
		"with\x00null.png":        "withnull.png",
		"/":                       "file",
		"":                        "file",
		// path.Base keeps the last real component, which is the sensible
		// reading of a name that happens to end in a separator.
		"ends/with/slash/":            "slash",
		"head\r\nContent-Type: x.png": "headContent-Type: x.png",
	}
	for input, want := range cases {
		if got := CleanFilename(input); got != want {
			t.Errorf("CleanFilename(%q) = %q, want %q", input, got, want)
		}
	}

	if got := CleanFilename(strings.Repeat("a", 400) + ".pdf"); len([]rune(got)) != 200 {
		t.Errorf("an overlong name was not trimmed: %d runes", len([]rune(got)))
	}
}

func TestBlockedExtensions(t *testing.T) {
	blocked := []string{"setup.exe", "run.BAT", "payload.js", "installer.msi", "thing.PS1"}
	for _, name := range blocked {
		if !IsBlockedFilename(name) {
			t.Errorf("%s should be refused", name)
		}
	}

	// Only the final extension decides what runs, which is the whole point of
	// the disguise.
	if !IsBlockedFilename("invoice.pdf.exe") {
		t.Error("a double extension slipped past the deny-list")
	}

	allowed := []string{"report.pdf", "sheet.xlsx", "photo.png", "notes.txt", "archive.zip"}
	for _, name := range allowed {
		if IsBlockedFilename(name) {
			t.Errorf("%s should be accepted", name)
		}
	}
}

func TestContentTypeAllowList(t *testing.T) {
	// The default: no list, everything the deny-list permits.
	if !ContentTypeAllowed("application/x-tar", nil) {
		t.Error("an empty allow-list should allow anything")
	}

	list := []string{"application/pdf", "image/*"}
	if !ContentTypeAllowed("application/pdf", list) {
		t.Error("an exact match was refused")
	}
	if !ContentTypeAllowed("image/png", list) {
		t.Error("a wildcard group match was refused")
	}
	// Parameters are not part of the decision.
	if !ContentTypeAllowed("APPLICATION/PDF; charset=binary", list) {
		t.Error("case and parameters should not affect the decision")
	}
	if ContentTypeAllowed("text/html", list) {
		t.Error("a type outside the list was allowed")
	}
	// A prefix that is not a group boundary must not match: "image/*" allows
	// image/png, not imagexml/anything.
	if ContentTypeAllowed("imagexml/thing", list) {
		t.Error("a wildcard matched across the type boundary")
	}
}
