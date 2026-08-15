package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("seeding %s: %v", path, err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestMirrorRewritesInPlaceAndKeepsEverythingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.env")
	write(t, path, strings.Join([]string{
		"# Mail server, provided by IT — ticket #4471",
		"SMTP_HOST=old.example.com",
		"",
		"# Not ours, do not touch",
		"SOME_OTHER_TOOL=keep-me",
	}, "\n"))

	mirror := NewEnvMirror(path)
	if err := mirror.Write(map[string]string{"SMTP_HOST": "new.example.com"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := read(t, path)
	if !strings.Contains(out, "SMTP_HOST=new.example.com") {
		t.Errorf("the managed key should be updated:\n%s", out)
	}
	if strings.Contains(out, "old.example.com") {
		t.Errorf("the old value should be gone:\n%s", out)
	}
	// Somebody wrote that comment to explain why the value is what it is.
	// Rewriting the file from a template would throw it away.
	if !strings.Contains(out, "ticket #4471") {
		t.Errorf("the operator's comment should survive:\n%s", out)
	}
	if !strings.Contains(out, "SOME_OTHER_TOOL=keep-me") {
		t.Errorf("unrelated variables must be left alone:\n%s", out)
	}
}

func TestMirrorAppendsKeysTheFileDoesNotHaveYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.env")
	write(t, path, "SOME_OTHER_TOOL=keep-me\n")

	mirror := NewEnvMirror(path)
	if err := mirror.Write(map[string]string{"AD_URL": "ldap://dc:389"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := read(t, path)
	if !strings.Contains(out, "AD_URL=ldap://dc:389") {
		t.Errorf("a missing key should be appended:\n%s", out)
	}
	if !strings.Contains(out, "SOME_OTHER_TOOL=keep-me") {
		t.Errorf("the existing content should survive:\n%s", out)
	}
}

func TestMirrorCreatesTheFileWhenItIsAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "app.env")
	mirror := NewEnvMirror(path)
	if err := mirror.Write(map[string]string{"SMTP_HOST": "smtp.example.com"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(read(t, path), "SMTP_HOST=smtp.example.com") {
		t.Error("expected the file to be created with the value")
	}
}

func TestMirrorQuotesOnlyWhenNecessary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.env")
	mirror := NewEnvMirror(path)
	if err := mirror.Write(map[string]string{
		"SMTP_FROM": "ProjectView <no-reply@example.com>",
		"AD_URL":    "ldap://dc:389",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := read(t, path)
	// A value with spaces has to be quoted or the file will not parse.
	if !strings.Contains(out, `SMTP_FROM="ProjectView <no-reply@example.com>"`) {
		t.Errorf("a value with spaces should be quoted:\n%s", out)
	}
	// One without stays bare, because the point of this file is that a person
	// can read and diff it.
	if !strings.Contains(out, "AD_URL=ldap://dc:389") {
		t.Errorf("a simple value should not be quoted:\n%s", out)
	}
}

func TestMirrorIsOffWithoutAPath(t *testing.T) {
	mirror := NewEnvMirror("")
	if mirror.Enabled() {
		t.Error("an empty path should disable mirroring")
	}
	if err := mirror.Write(map[string]string{"SMTP_HOST": "x"}); err != nil {
		t.Errorf("a disabled mirror should be a no-op, got %v", err)
	}
}

func TestMirrorLeavesNoTemporaryFilesBehind(t *testing.T) {
	// The write goes through a temporary file and a rename so a crash cannot
	// leave a half-written .env, which is a file that can stop a stack booting.
	dir := t.TempDir()
	path := filepath.Join(dir, "app.env")
	mirror := NewEnvMirror(path)
	for i := 0; i < 3; i++ {
		if err := mirror.Write(map[string]string{"SMTP_HOST": "smtp.example.com"}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "app.env" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only app.env, found %v", names)
	}
}

func TestKeyOf(t *testing.T) {
	cases := map[string]string{
		"SMTP_HOST=x":        "SMTP_HOST",
		"  SMTP_HOST = x  ":  "SMTP_HOST",
		"export SMTP_HOST=x": "SMTP_HOST",
		"# SMTP_HOST=x":      "",
		"":                   "",
		"   ":                "",
		"not an assignment":  "",
		"=novalue":           "",
	}
	for line, want := range cases {
		if got := keyOf(line); got != want {
			t.Errorf("keyOf(%q) = %q, want %q", line, got, want)
		}
	}
}
