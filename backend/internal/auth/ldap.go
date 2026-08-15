package auth

import (
	"crypto/tls"
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

	tlsConfig := &tls.Config{InsecureSkipVerify: !ad.TLSRejectUnauthorized} // nolint:gosec

	dial := func() (*ldap.Conn, error) {
		if strings.HasPrefix(ad.URL, "ldaps://") {
			return ldap.DialURL(ad.URL, ldap.DialWithTLSConfig(tlsConfig))
		}
		return ldap.DialURL(ad.URL)
	}

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
