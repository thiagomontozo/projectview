package db

import "testing"

// The masked DSN is written to the application log on every boot, so a leak
// here would put the database password in plain text in the container logs.
func TestMaskURL(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		want string
	}{
		{
			"password is masked",
			"postgres://projectview:sup3rs3cret@postgres:5432/projectview?sslmode=disable",
			"postgres://projectview:****@postgres:5432/projectview?sslmode=disable",
		},
		{
			"DSN without credentials is untouched",
			"postgres://postgres:5432/projectview",
			"postgres://postgres:5432/projectview",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := maskURL(tc.dsn); got != tc.want {
				t.Errorf("maskURL(%q) = %q, want %q", tc.dsn, got, tc.want)
			}
		})
	}
}

func TestMaskURLDoesNotLeakPassword(t *testing.T) {
	const password = "sup3rs3cret"
	masked := maskURL("postgres://projectview:" + password + "@postgres:5432/projectview")
	if contains(masked, password) {
		t.Fatalf("maskURL leaked the password: %q", masked)
	}
}

func TestDatabaseName(t *testing.T) {
	cases := []struct {
		dsn  string
		want string
	}{
		{"postgres://user:pass@host:5432/projectview?sslmode=disable", "projectview"},
		{"postgres://host:5432/custom_db", "custom_db"},
		{"postgres://host:5432/", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := DatabaseName(tc.dsn); got != tc.want {
			t.Errorf("DatabaseName(%q) = %q, want %q", tc.dsn, got, tc.want)
		}
	}
}

// Migrations are embedded in the binary; if the embed directive ever stops
// matching, the schema silently would never be created on a fresh database.
func TestMigrationsAreEmbedded(t *testing.T) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatalf("migrations directory is not embedded: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migration files were embedded")
	}

	found := false
	for _, e := range entries {
		if e.Name() == "0001_init.sql" {
			found = true
			body, err := migrationFiles.ReadFile("migrations/" + e.Name())
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
			// Spot-check that the core tables really are in there.
			for _, table := range []string{"users", "teams", "projects", "tasks",
				"chat_channels", "chat_messages", "notifications"} {
				if !contains(string(body), "CREATE TABLE "+table+" (") {
					t.Errorf("initial migration does not create table %q", table)
				}
			}
		}
	}
	if !found {
		t.Error("0001_init.sql is missing from the embedded migrations")
	}
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
