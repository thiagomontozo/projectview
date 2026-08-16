package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AWS Signature Version 4
// =======================
//
// Written directly rather than pulled from an SDK, for the same reason the
// OIDC flow is: this is one hash chain and two string formats, while the
// alternative brings a credential-resolution chain, a retry policy and a
// middleware stack, none of which this application wants an opinion from. The
// whole surface used here is PUT, DELETE, HEAD and a presigned GET.
//
// Two signing modes exist and both are needed:
//
//   - header authorization, for the calls the backend makes itself
//   - query authorization, for the time-limited URL handed to a browser
//
// They share everything except where the parameters travel, which is why the
// canonical request is built once below and used by both.

const (
	algorithm       = "AWS4-HMAC-SHA256"
	unsignedPayload = "UNSIGNED-PAYLOAD"
	amzDateFormat   = "20060102T150405Z"
	dateFormat      = "20060102"
)

// credentialScope is the part of the signature that binds it to one day, one
// region and one service - which is what stops a signature captured today from
// being replayed against another bucket tomorrow.
func credentialScope(now time.Time, region string) string {
	return strings.Join([]string{now.UTC().Format(dateFormat), region, "s3", "aws4_request"}, "/")
}

// signingKey derives the per-day key. The secret itself never signs anything
// directly: it seeds a four-step chain, so a leaked signing key expires with
// the day it was derived for.
func signingKey(secret string, now time.Time, region string) []byte {
	key := hmacSHA256([]byte("AWS4"+secret), now.UTC().Format(dateFormat))
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	return hmacSHA256(key, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// canonicalRequest is the exact document both sides hash. Any disagreement
// about escaping, header order or trailing newlines produces a signature
// mismatch and nothing else, which is why every rule here is explicit rather
// than delegated to net/url.
func canonicalRequest(method, path, query string, headers map[string]string, payloadHash string) (string, string) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)

	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteString(":")
		// Values are trimmed and internal runs of spaces collapsed, per the
		// specification. Ours are simple, but a Content-Type with a padded
		// parameter would otherwise sign differently than it transmits.
		canonicalHeaders.WriteString(strings.Join(strings.Fields(headers[name]), " "))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(names, ";")

	request := strings.Join([]string{
		method,
		path,
		query,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	return request, signedHeaders
}

func stringToSign(now time.Time, region, canonical string) string {
	return strings.Join([]string{
		algorithm,
		now.UTC().Format(amzDateFormat),
		credentialScope(now, region),
		sha256Hex([]byte(canonical)),
	}, "\n")
}

// escapePath percent-encodes a URI path the way S3 expects: every byte outside
// the unreserved set, except the separators, which stay literal. net/url is
// deliberately not used - url.URL re-normalises paths on String(), and a path
// that changes shape between signing and sending is a 403 nobody can read.
func escapePath(path string) string {
	var out strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch {
		case c == '/':
			out.WriteByte(c)
		case isUnreserved(c):
			out.WriteByte(c)
		default:
			out.WriteString("%")
			out.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return out.String()
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

// canonicalQuery sorts and encodes the query string. Sorting is part of the
// signature, not a tidiness preference: the signer and the verifier have to
// walk the parameters in the same order to arrive at the same hash.
func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(values))
	for _, key := range keys {
		items := append([]string(nil), values[key]...)
		sort.Strings(items)
		for _, item := range items {
			parts = append(parts, escapeQuery(key)+"="+escapeQuery(item))
		}
	}
	return strings.Join(parts, "&")
}

// escapeQuery is RFC 3986 encoding. url.QueryEscape is not usable here: it
// encodes a space as "+", which AWS rejects, and leaves some characters alone
// that have to be escaped.
func escapeQuery(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			out.WriteByte(c)
			continue
		}
		out.WriteString("%")
		out.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
	}
	return out.String()
}

func signature(secret string, now time.Time, region, toSign string) string {
	return hex.EncodeToString(hmacSHA256(signingKey(secret, now, region), toSign))
}
