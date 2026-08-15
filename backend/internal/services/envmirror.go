package services

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"projectview/internal/config"
	"projectview/internal/logger"
)

// EnvMirror keeps a .env-shaped copy of the saved settings on disk.
//
// The database is authoritative — it is what the process reads on boot and
// what a save applies immediately. This file exists so the configuration is
// visible, backed up and reviewable outside the application, and so an
// operator rebuilding the stack elsewhere has something to start from.
//
// It is deliberately not what the container reads. Compose interpolates its
// environment once, at parse time, and Go reads it once, at boot: a file the
// application rewrote would take a restart to matter, which is exactly the
// delay the settings screen exists to remove.
type EnvMirror struct {
	path string
}

// NewEnvMirror returns a mirror writing to path, or a disabled one when the
// path is empty.
func NewEnvMirror(path string) *EnvMirror { return &EnvMirror{path: strings.TrimSpace(path)} }

func (m *EnvMirror) Enabled() bool { return m.path != "" }

func (m *EnvMirror) Path() string { return m.path }

// Write updates the managed keys in the file, leaving every other line — other
// variables, blank lines and the operator's own comments — exactly as it found
// them. Rewriting the whole file from a template would silently discard
// somebody's notes about why a value is what it is.
//
// Returns an error the caller should report but not treat as a failed save:
// the setting is already committed to the database by then, and refusing the
// whole operation because a mount is read-only would be worse than a warning.
func (m *EnvMirror) Write(values map[string]string) error {
	if !m.Enabled() {
		return nil
	}

	managed := map[string]bool{}
	for _, key := range config.ManagedKeys() {
		managed[key] = true
	}

	existing, err := os.ReadFile(m.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", m.path, err)
	}

	var out []string
	seen := map[string]bool{}

	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		key := keyOf(line)
		if key == "" || !managed[key] {
			out = append(out, line)
			continue
		}
		// Rewrite in place, so the key keeps the position and the comment
		// block the operator put it in.
		out = append(out, key+"="+quote(values[key]))
		seen[key] = true
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", m.path, err)
	}

	missing := make([]string, 0)
	for _, key := range config.ManagedKeys() {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, "# --- Written by ProjectView's settings screen ---------------------------",
			"# The database is authoritative; this file is a mirror for backup and",
			"# review. Editing it by hand does not change a running installation.")
		for _, key := range missing {
			out = append(out, key+"="+quote(values[key]))
		}
	}

	return m.replace(strings.Join(out, "\n") + "\n")
}

// replace writes through a temporary file in the same directory and renames
// it over the target. A half-written .env is a file that can stop a stack from
// booting, and a crash mid-write must not be able to produce one.
func (m *EnvMirror) replace(contents string) error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("preparing %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".env-*")
	if err != nil {
		return fmt.Errorf("writing near %s: %w", m.path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.WriteString(contents); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Secrets live here, so it is readable by its owner and nobody else.
	if err := os.Chmod(tmpName, 0o600); err != nil {
		logger.Warn("env mirror: could not tighten permissions on %s: %v", tmpName, err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("replacing %s: %w", m.path, err)
	}
	return nil
}

// keyOf returns the variable a line assigns, or "" for comments and blanks.
func keyOf(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	name, _, found := strings.Cut(trimmed, "=")
	if !found {
		return ""
	}
	name = strings.TrimSpace(strings.TrimPrefix(name, "export "))
	if name == "" {
		return ""
	}
	return name
}

// quote wraps a value only when it needs it. An unquoted value is easier to
// read and to diff, which is most of the point of keeping this file.
func quote(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t\"'#$`\\") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
	}
	return value
}
