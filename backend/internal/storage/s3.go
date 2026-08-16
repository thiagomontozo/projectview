// Package storage puts attachment bytes somewhere that is not the database.
//
// The target is any S3-compatible object store: MinIO in the bundled Compose
// stack, Amazon S3 or an equivalent in production. Only four operations are
// used - PUT, DELETE, HEAD, and a presigned GET - so the client is written
// directly against the HTTP API rather than through an SDK.
//
// Files are never served from the application. A download is a time-limited
// signed URL the browser fetches from the object store itself, which keeps
// megabytes out of the API process and means a link that leaks stops working
// on its own. Nothing in the bucket is public.
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrDisabled is returned by every operation when no object store is
// configured. Callers surface it as "attachments are not available here"
// rather than as a failure, because an installation without object storage is
// a supported deployment, not a broken one.
var ErrDisabled = errors.New("object storage is not configured")

type Config struct {
	// Endpoint is the address the backend itself calls, e.g.
	// http://minio:9000 on the internal Compose network.
	Endpoint string
	// PublicURL is the base a browser can reach, used when signing download
	// links. It differs from Endpoint whenever the store sits behind the edge
	// proxy, which is the bundled arrangement. Empty falls back to Endpoint.
	//
	// This must be the host the browser actually requests: the signature
	// covers the Host header, so signing for "minio:9000" and fetching from
	// "localhost" fails verification.
	PublicURL string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// ForcePathStyle addresses the bucket as a path segment
	// (endpoint/bucket/key) rather than as a subdomain. Required by MinIO and
	// by any deployment whose endpoint is an IP address.
	ForcePathStyle bool
}

type S3 struct {
	cfg      Config
	endpoint *url.URL
	public   *url.URL
	client   *http.Client
}

// New builds a client, or returns nil when the configuration is incomplete.
// A nil *S3 is a working value: every method answers ErrDisabled, so the
// callers do not each need a nil check.
func New(cfg Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, nil
	}

	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid storage endpoint %q: %w", cfg.Endpoint, err)
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("storage endpoint %q needs a scheme and a host", cfg.Endpoint)
	}

	public := endpoint
	if cfg.PublicURL != "" {
		public, err = url.Parse(cfg.PublicURL)
		if err != nil {
			return nil, fmt.Errorf("invalid storage public URL %q: %w", cfg.PublicURL, err)
		}
	}

	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	return &S3{
		cfg:      cfg,
		endpoint: endpoint,
		public:   public,
		// Uploads are bounded by the attachment size limit, so a generous but
		// finite timeout is right: an object store that has stopped answering
		// must not hold an API request open indefinitely.
		client: &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

func (s *S3) Enabled() bool { return s != nil }

// Bucket reports which bucket the attachments live in, for the operator-facing
// parts of the settings and health output.
func (s *S3) Bucket() string {
	if s == nil {
		return ""
	}
	return s.cfg.Bucket
}

// objectPath is the URI path of one object, already escaped for signing.
func (s *S3) objectPath(base *url.URL, key string) string {
	prefix := strings.TrimSuffix(base.Path, "/")
	if s.cfg.ForcePathStyle {
		return prefix + "/" + s.cfg.Bucket + escapePath("/"+key)
	}
	return prefix + escapePath("/"+key)
}

func (s *S3) host(base *url.URL) string {
	if s.cfg.ForcePathStyle {
		return base.Host
	}
	return s.cfg.Bucket + "." + base.Host
}

// Put stores an object. The body is a byte slice rather than a reader because
// the caller has already buffered it to enforce the size limit and to hand it
// to the virus-scan hook - streaming here would mean doing both twice.
func (s *S3) Put(ctx context.Context, key string, body []byte, contentType string) error {
	if s == nil {
		return ErrDisabled
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	now := time.Now().UTC()
	path := s.objectPath(s.endpoint, key)
	payloadHash := sha256Hex(body)

	headers := map[string]string{
		"host":                 s.host(s.endpoint),
		"content-type":         contentType,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           now.Format(amzDateFormat),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		s.endpoint.Scheme+"://"+s.host(s.endpoint)+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(body))
	s.signHeaders(req, headers, now, http.MethodPut, path, "", payloadHash)

	return s.do(req, "store")
}

// Delete removes an object. A key that is already gone is not an error: the
// deferred-delete queue retries, and a retry after a partial success must be
// able to finish rather than fail forever on the step that already worked.
func (s *S3) Delete(ctx context.Context, key string) error {
	if s == nil {
		return ErrDisabled
	}

	now := time.Now().UTC()
	path := s.objectPath(s.endpoint, key)

	headers := map[string]string{
		"host":                 s.host(s.endpoint),
		"x-amz-content-sha256": sha256Hex(nil),
		"x-amz-date":           now.Format(amzDateFormat),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		s.endpoint.Scheme+"://"+s.host(s.endpoint)+path, nil)
	if err != nil {
		return err
	}
	s.signHeaders(req, headers, now, http.MethodDelete, path, "", sha256Hex(nil))

	return s.do(req, "delete")
}

// Check verifies the bucket exists and the credentials are accepted. Called at
// boot so a misconfiguration is a log line at startup rather than a failed
// upload discovered by the first person who tries to attach something.
func (s *S3) Check(ctx context.Context) error {
	if s == nil {
		return ErrDisabled
	}

	now := time.Now().UTC()
	base := strings.TrimSuffix(s.endpoint.Path, "/")
	path := base + "/"
	if s.cfg.ForcePathStyle {
		path = base + "/" + s.cfg.Bucket
	}

	headers := map[string]string{
		"host":                 s.host(s.endpoint),
		"x-amz-content-sha256": sha256Hex(nil),
		"x-amz-date":           now.Format(amzDateFormat),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		s.endpoint.Scheme+"://"+s.host(s.endpoint)+path, nil)
	if err != nil {
		return err
	}
	s.signHeaders(req, headers, now, http.MethodHead, path, "", sha256Hex(nil))

	return s.do(req, "reach")
}

// PresignGet builds a time-limited download URL.
//
// The original filename travels as a signed response-content-disposition
// parameter, so the browser saves "specification.pdf" rather than the opaque
// storage key - and because it is signed, a recipient cannot rewrite it into
// something that looks like an executable.
func (s *S3) PresignGet(key, filename, contentType string, ttl time.Duration) (string, time.Time, error) {
	return s.presignGetAt(time.Now().UTC(), key, filename, contentType, ttl)
}

// presignGetAt is PresignGet with the clock passed in, so the signature can be
// checked against the published AWS test vectors, which are all fixed at one
// instant.
func (s *S3) presignGetAt(now time.Time, key, filename, contentType string, ttl time.Duration) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, ErrDisabled
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	now = now.UTC()
	path := s.objectPath(s.public, key)
	host := s.host(s.public)

	query := url.Values{}
	query.Set("X-Amz-Algorithm", algorithm)
	query.Set("X-Amz-Credential", s.cfg.AccessKey+"/"+credentialScope(now, s.cfg.Region))
	query.Set("X-Amz-Date", now.Format(amzDateFormat))
	query.Set("X-Amz-Expires", strconv.Itoa(int(ttl.Seconds())))
	query.Set("X-Amz-SignedHeaders", "host")
	if filename != "" {
		query.Set("response-content-disposition", contentDisposition(filename))
	}
	if contentType != "" {
		// Without this the store replays whatever Content-Type it recorded at
		// upload; pinning it keeps an image rendering inline and everything
		// else downloading, regardless of what was stored.
		query.Set("response-content-type", contentType)
	}

	canonical, _ := canonicalRequest(http.MethodGet, path, canonicalQuery(query),
		map[string]string{"host": host}, unsignedPayload)
	query.Set("X-Amz-Signature", signature(s.cfg.SecretKey, now, s.cfg.Region,
		stringToSign(now, s.cfg.Region, canonical)))

	signed := s.public.Scheme + "://" + host + path + "?" + canonicalQuery(query)
	return signed, now.Add(ttl), nil
}

// contentDisposition names the file for the browser.
//
// Images are shown inline so a screenshot pasted onto a task can be looked at
// without a round trip through the downloads folder; everything else is an
// attachment. The filename is quoted with the quotes and backslashes escaped,
// and repeated as RFC 5987 UTF-8 so a name with accents survives - "relatório
// final.pdf" arriving as "relatrio final.pdf" is the usual symptom of skipping
// that second form.
func contentDisposition(filename string) string {
	disposition := "attachment"
	if IsInlineRenderable(filename) {
		disposition = "inline"
	}

	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filename)
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return '_'
		}
		return r
	}, escaped)

	return disposition + `; filename="` + ascii + `"; filename*=UTF-8''` + escapeQuery(filename)
}

// signHeaders applies the Authorization header form of SigV4.
func (s *S3) signHeaders(req *http.Request, headers map[string]string, now time.Time, method, path, query, payloadHash string) {
	canonical, signedHeaders := canonicalRequest(method, path, query, headers, payloadHash)
	sig := signature(s.cfg.SecretKey, now, s.cfg.Region, stringToSign(now, s.cfg.Region, canonical))

	for name, value := range headers {
		if name == "host" {
			// net/http refuses Host as an ordinary header and would drop it.
			req.Host = value
			continue
		}
		req.Header.Set(name, value)
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, s.cfg.AccessKey, credentialScope(now, s.cfg.Region), signedHeaders, sig))
}

func (s *S3) do(req *http.Request, verb string) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not %s object: %w", verb, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// A DELETE of something already absent has done its job.
	if req.Method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
		return nil
	}

	// The store answers errors as XML. It is passed through rather than parsed:
	// the codes ("NoSuchBucket", "SignatureDoesNotMatch") are what an operator
	// needs, and modelling the envelope to re-emit the same string would only
	// add a way to lose it.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("could not %s object: %s: %s", verb, resp.Status,
		strings.TrimSpace(string(body)))
}
