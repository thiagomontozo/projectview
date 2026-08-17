package auth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"projectview/internal/config"
	"projectview/internal/logger"
)

// Profile is the normalized user information extracted from Active Directory
// after a successful authentication.
type Profile struct {
	Username string
	Name     string
	Email    string
}

func escapeFilter(v string) string {
	return ldap.EscapeFilter(v)
}

// DirectoryEntry is one person found by searching the directory, before they
// have any account here.
type DirectoryEntry struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// ErrDirectorySearchUnavailable is returned when the directory cannot be
// searched at all, as opposed to being searched and finding nobody. The two
// mean very different things to whoever is looking at an empty list.
var ErrDirectorySearchUnavailable = errors.New("directory search is not available")

// MinDirectoryQuery is the shortest query that will be run.
//
// Not a performance guard. A one-character search against a corporate
// directory returns thousands of people, which is both useless to the person
// typing and an efficient way to extract the staff list one letter at a time.
const MinDirectoryQuery = 2

// SearchDirectory finds people in AD by name, username or e-mail.
//
// This is what makes it possible to put a colleague on a team before they have
// ever signed in. Without it the only way into the local user table is
// just-in-time provisioning at first login, so an administrator building a team
// could only choose from people who had already logged in - which is the wrong
// way round, since being put on the team is often why somebody logs in.
//
// It requires the service account (AD_BIND_DN / AD_BIND_PASSWORD). There is no
// way around that: searching a directory needs credentials of its own, and the
// only other ones this application ever sees belong to the person signing in,
// at the moment they sign in. When no service account is configured the caller
// is told the search is unavailable rather than shown an empty result, because
// "nobody matched" and "nobody could be looked up" must not look the same.
func SearchDirectory(cfg *config.Config, query string, limit int) ([]DirectoryEntry, error) {
	ad := cfg.AD()

	if !ad.Enabled {
		return nil, fmt.Errorf("%w: Active Directory is not enabled", ErrDirectorySearchUnavailable)
	}
	if ad.BindDN == "" || ad.BindPassword == "" {
		return nil, fmt.Errorf("%w: no service account is configured (AD_BIND_DN / AD_BIND_PASSWORD)",
			ErrDirectorySearchUnavailable)
	}

	query = strings.TrimSpace(query)
	if len([]rune(query)) < MinDirectoryQuery {
		return []DirectoryEntry{}, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	conn, err := dialAD(ad)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDirectorySearchUnavailable, err)
	}
	defer conn.Close()

	if err := conn.Bind(ad.BindDN, ad.BindPassword); err != nil {
		logger.Warn("AD service account bind failed during a directory search: %v", err)
		return nil, fmt.Errorf("%w: the service account was refused", ErrDirectorySearchUnavailable)
	}

	// The query is escaped first and the wildcards added afterwards, in that
	// order. Escaping after would neutralise our own asterisks; adding them
	// before would let a caller's asterisk through and turn a search box into
	// an arbitrary LDAP filter.
	needle := escapeFilter(query)
	filter := fmt.Sprintf(
		"(&(objectClass=person)(|(%s=*%s*)(cn=*%s*)(displayName=*%s*)(mail=*%s*)))",
		ad.UsernameAttribute, needle, needle, needle, needle)

	result, err := conn.Search(ldap.NewSearchRequest(
		ad.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		// The server applies the size limit, so an over-broad search costs one
		// bounded response rather than a directory dump we then trim.
		limit, 15, false,
		filter,
		[]string{"cn", "mail", "displayName", ad.UsernameAttribute},
		nil,
	))
	if err != nil {
		// A size-limit-exceeded result still carries the entries it found, so
		// it is a successful partial answer rather than a failure.
		if !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrDirectorySearchUnavailable, err)
		}
	}
	if result == nil {
		return []DirectoryEntry{}, nil
	}

	out := make([]DirectoryEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		username := entry.GetAttributeValue(ad.UsernameAttribute)
		if username == "" {
			// Without the attribute that identifies them at login there is
			// nothing to provision the account against.
			continue
		}
		out = append(out, DirectoryEntry{
			Username: strings.ToLower(username),
			Name:     displayNameOf(entry, username),
			Email:    strings.ToLower(emailOf(entry, username, ad.Domain)),
		})
	}
	return out, nil
}

func displayNameOf(entry *ldap.Entry, fallback string) string {
	if name := entry.GetAttributeValue("displayName"); name != "" {
		return name
	}
	if name := entry.GetAttributeValue("cn"); name != "" {
		return name
	}
	return fallback
}

func emailOf(entry *ldap.Entry, username, domain string) string {
	if mail := entry.GetAttributeValue("mail"); mail != "" {
		return mail
	}
	if domain == "" {
		return ""
	}
	return username + "@" + domain
}

// dialAD opens a connection, honouring ldaps:// and the certificate policy.
func dialAD(ad config.ADConfig) (*ldap.Conn, error) {
	if strings.HasPrefix(ad.URL, "ldaps://") {
		tlsConfig := &tls.Config{InsecureSkipVerify: !ad.TLSRejectUnauthorized} // nolint:gosec
		return ldap.DialURL(ad.URL, ldap.DialWithTLSConfig(tlsConfig))
	}
	return ldap.DialURL(ad.URL)
}

// AuthenticateAD authenticates a user against Active Directory / LDAP.
//
// Strategy:
//  1. If AD_BIND_DN/AD_BIND_PASSWORD are configured, bind with the service
//     account first and search the directory for the user's entry (so login
//     works with just the sAMAccountName, independent of the bind format).
//  2. Otherwise, attempt a direct bind as "<username>@<AD_DOMAIN>"
//     (userPrincipalName), the simplest setup for a single domain.
func AuthenticateAD(cfg *config.Config, username, password string) (*Profile, error) {
	// One snapshot per attempt: a settings change mid-login must not leave
	// this function binding with one server and searching another.
	ad := cfg.AD()

	if !ad.Enabled {
		return nil, fmt.Errorf("AD authentication is not enabled on this server")
	}
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	dial := func() (*ldap.Conn, error) { return dialAD(ad) }

	conn, err := dial()
	if err != nil {
		return nil, fmt.Errorf("could not connect to AD/LDAP server: %w", err)
	}
	defer conn.Close()

	var profile *Profile

	if ad.BindDN != "" && ad.BindPassword != "" {
		if err := conn.Bind(ad.BindDN, ad.BindPassword); err != nil {
			logger.Warn("AD service account bind failed: %v", err)
			return nil, fmt.Errorf("invalid Active Directory credentials")
		}

		filter := fmt.Sprintf("(%s=%s)", ad.UsernameAttribute, escapeFilter(username))
		searchReq := ldap.NewSearchRequest(
			ad.BaseDN,
			ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
			filter,
			[]string{"dn", "cn", "mail", "displayName", ad.UsernameAttribute},
			nil,
		)

		result, err := conn.Search(searchReq)
		if err != nil || len(result.Entries) == 0 {
			return nil, fmt.Errorf("user not found in Active Directory")
		}
		entry := result.Entries[0]

		email := entry.GetAttributeValue("mail")
		if email == "" {
			email = username + "@" + ad.Domain
		}
		name := entry.GetAttributeValue("displayName")
		if name == "" {
			name = entry.GetAttributeValue("cn")
		}
		if name == "" {
			name = username
		}
		profile = &Profile{Username: strings.ToLower(username), Name: name, Email: strings.ToLower(email)}

		// Re-bind as the actual user (on a fresh connection) to verify the password.
		userConn, err := dial()
		if err != nil {
			return nil, fmt.Errorf("could not connect to AD/LDAP server: %w", err)
		}
		defer userConn.Close()
		if err := userConn.Bind(entry.DN, password); err != nil {
			logger.Warn("AD authentication failed for %q: %v", username, err)
			return nil, fmt.Errorf("invalid Active Directory credentials")
		}
	} else {
		upn := username
		if !strings.Contains(username, "@") {
			upn = username + "@" + ad.Domain
		}
		if err := conn.Bind(upn, password); err != nil {
			logger.Warn("AD authentication failed for %q: %v", username, err)
			return nil, fmt.Errorf("invalid Active Directory credentials")
		}
		localPart := strings.Split(upn, "@")[0]
		profile = &Profile{Username: strings.ToLower(localPart), Name: localPart, Email: strings.ToLower(upn)}
	}

	return profile, nil
}
