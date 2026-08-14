package db

import "testing"

func TestDBNameFromURI(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want string
	}{
		{"plain host and database", "mongodb://mongo:27017/pm_dashboard", "pm_dashboard"},
		{"database with query options", "mongodb://mongo:27017/pm_dashboard?retryWrites=true&w=majority", "pm_dashboard"},
		{"custom database name is honoured", "mongodb://mongo:27017/projectview_prod", "projectview_prod"},
		{"credentials in the URI", "mongodb://user:pass@mongo:27017/pm_dashboard", "pm_dashboard"},
		{"srv scheme", "mongodb+srv://user:pass@cluster0.abcde.mongodb.net/projectview?retryWrites=true", "projectview"},
		{"replica set with several hosts", "mongodb://a:27017,b:27017,c:27017/pm_dashboard?replicaSet=rs0", "pm_dashboard"},
		{"percent-encoded database name", "mongodb://mongo:27017/my%20db", "my db"},
		{"no database in the path", "mongodb://mongo:27017", ""},
		{"trailing slash but no database", "mongodb://mongo:27017/", ""},
		{"options but no database", "mongodb://mongo:27017/?replicaSet=rs0", ""},
		{"unknown scheme", "postgres://localhost:5432/app", ""},
		{"empty string", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dbNameFromURI(tc.uri); got != tc.want {
				t.Errorf("dbNameFromURI(%q) = %q, want %q", tc.uri, got, tc.want)
			}
		})
	}
}

func TestMaskURI(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want string
	}{
		{
			"password is masked",
			"mongodb://admin:sup3rs3cret@mongo:27017/pm_dashboard",
			"mongodb://admin:****@mongo:27017/pm_dashboard",
		},
		{
			"URI without credentials is untouched",
			"mongodb://mongo:27017/pm_dashboard",
			"mongodb://mongo:27017/pm_dashboard",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maskURI(tc.uri)
			if got != tc.want {
				t.Errorf("maskURI(%q) = %q, want %q", tc.uri, got, tc.want)
			}
		})
	}
}

// The masked URI is written to the application log on every boot, so a leak
// here would put the database password in plain text in the container logs.
func TestMaskURIDoesNotLeakPassword(t *testing.T) {
	const password = "sup3rs3cret"
	masked := maskURI("mongodb://admin:" + password + "@mongo:27017/pm_dashboard")
	if contains(masked, password) {
		t.Fatalf("maskURI leaked the password: %q", masked)
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
