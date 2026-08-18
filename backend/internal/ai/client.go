// Package ai talks to an OpenAI-compatible chat completions endpoint.
//
// The endpoint is a URL an administrator types into the settings screen, and
// the wire format is the de-facto standard one: POST {base}/chat/completions
// with a Bearer key. That is spoken by OpenAI, Azure OpenAI, vLLM, Ollama,
// llama.cpp and OpenRouter alike, so the same client serves a hosted model and
// one running in a container beside this stack. Which of those an installation
// uses is a deployment decision - and, for a tool holding a company's work, a
// governance one - so it is configuration rather than code.
//
// Written directly rather than through an SDK, for the reason the S3 client
// gives: the surface used here is one endpoint, while the official library
// brings a credential chain, a retry policy and a middleware stack this
// application already has opinions about.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrDisabled is returned when no model is configured. A deployment without one
// is supported, not broken: every caller degrades to doing nothing.
var ErrDisabled = errors.New("no model is configured")

type Config struct {
	Endpoint string
	APIKey   string
	Model    string
	Timeout  time.Duration
}

type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a client, or returns nil when there is no endpoint. A nil *Client
// is a working value: every method answers ErrDisabled, so callers need no nil
// check of their own.
//
// The model name is optional. Left empty it is discovered from the endpoint's
// own /models route, which is part of the same standard - see models.go.
func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, nil
	}
	// Normalised before it is validated, in that order. The other way round
	// refuses "192.168.1.50:8000" for having no scheme - which is exactly the
	// input this is meant to accept.
	cfg.Endpoint = NormalizeEndpoint(cfg.Endpoint)
	if err := ValidateEndpoint(cfg.Endpoint); err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

func (c *Client) Enabled() bool { return c != nil }

// Model is the name that was configured, which is empty when it is being
// discovered. What a request actually used comes back on the Result, since
// that is the only value that is true for certain.
func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.cfg.Model
}

// ValidateEndpoint checks a URL before the server will ever fetch it.
//
// This is the one setting on the screen that makes the backend issue a request
// to an address somebody typed, which is server-side request forgery by
// definition. The screen is administrators-only, but the file that governs it
// already reasons about exactly this: an installation that can be repointed
// from a web form is one compromised administrator away from being somebody
// else's.
//
// Private and loopback addresses are deliberately *allowed* - the whole point
// is that a model may run in a container beside this one, at http://ollama:11434
// - so the guard is narrow rather than broad: the cloud metadata addresses,
// which every major provider exposes unauthenticated to anything that can make
// an HTTP request from inside the instance, and which have no legitimate use as
// a completions endpoint.
func ValidateEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("the model endpoint is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("the model endpoint must be an http or https URL")
	}
	if parsed.Host == "" {
		return errors.New("the model endpoint needs a host")
	}

	host := parsed.Hostname()
	// Checked as a literal as well as resolved: a name that resolves to the
	// metadata address is the same request by a different spelling.
	for _, candidate := range append([]string{host}, resolve(host)...) {
		if isMetadataAddress(candidate) {
			return errors.New("that address is the cloud metadata service, not a model endpoint")
		}
	}
	return nil
}

// resolve looks the host up, and returns nothing on failure. A name that does
// not resolve now is refused later by the request itself; failing the whole
// save because DNS blinked would be worse than letting it through.
func resolve(host string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil
	}
	return addrs
}

func isMetadataAddress(candidate string) bool {
	ip := net.ParseIP(candidate)
	if ip == nil {
		return false
	}
	// 169.254.169.254 (AWS, GCP, Azure, DigitalOcean) and the IPv6 form.
	return ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("fd00:ec2::254"))
}

// NormalizeEndpoint turns what somebody types into a base URL this client can
// use.
//
// The protocol is the code's problem, not the administrator's: they know the
// address of their inference host, and should be able to write "192.168.1.50:8000"
// and be done. Two rules, both chosen to be predictable rather than clever.
//
// A missing scheme becomes http for an address on the local network and https
// for anything else - which matches how those two are actually deployed: an
// internal box on a private range rarely has a certificate, and a public
// provider always does.
//
// A missing path becomes /v1, because that is where every OpenAI-compatible
// server mounts its routes and forgetting it is the commonest mistake there is.
// A path that was supplied is left exactly alone: Azure OpenAI serves from
// /openai/deployments/{name}, and "helpfully" appending /v1 to that would break
// the one case where the person definitely knew what they were doing.
func NormalizeEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if !strings.Contains(raw, "://") {
		scheme := "https://"
		if isLocalAddress(raw) {
			scheme = "http://"
		}
		raw = scheme + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.Trim(parsed.Path, "/") == "" {
		parsed.Path = "/v1"
	}
	return strings.TrimSuffix(parsed.String(), "/")
}

// isLocalAddress reports whether an address looks like something on the local
// network, which is the case that is almost never behind TLS.
func isLocalAddress(hostport string) bool {
	host := hostport
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		// A bare name with no dots is a container or a host on the LAN -
		// "ollama", "ai-server" - never a public domain.
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return false
}

// Message is one turn of the conversation sent to the model.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type completionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	// Low rather than zero: the task here is classification against a fixed
	// list, where creativity is not a feature. Zero is not portable - some
	// compatible servers reject it - so this is the smallest widely accepted
	// value.
	Temperature float64 `json:"temperature"`
	// Bounds a runaway response. The answers wanted here are a few dozen
	// tokens; anything longer is a model that has misunderstood the task.
	MaxTokens int `json:"max_tokens"`
	// Asks for JSON where the server supports it. Not relied upon: the output
	// is parsed and validated regardless, because "JSON mode" is advisory on
	// several compatible servers and absent on others.
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type completionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Result is what one completion produced, with what it cost and which model
// produced it - the last of these because the model may have been discovered
// rather than configured, and a suggestion has to record what actually
// answered it.
type Result struct {
	Content string
	Tokens  int
	Model   string
}

// Complete sends a conversation and returns the reply.
//
// No retry. A model that refused or timed out is not obviously better on a
// second attempt, and the caller here is a background sweep that will come
// round again anyway - so a retry loop would multiply the bill without
// improving the answer.
func (c *Client) Complete(ctx context.Context, messages []Message, jsonOutput bool) (*Result, error) {
	if c == nil {
		return nil, ErrDisabled
	}

	model, err := c.resolveModel(ctx)
	if err != nil {
		return nil, err
	}

	payload := completionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   500,
	}
	if jsonOutput {
		payload.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// The base is already normalised by New, so this is only the last segment.
	endpoint := strings.TrimSuffix(c.cfg.Endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		// A local model usually needs no key, so an empty one sends no header
		// rather than an empty Bearer, which some servers reject outright.
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the model could not be reached: %w", err)
	}
	defer resp.Body.Close()

	// Bounded: a compatible server that streams or misbehaves must not be able
	// to hand this process an unbounded body.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the model answered %s: %s", resp.Status,
			strings.TrimSpace(string(raw)))
	}

	var parsed completionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("the model's reply was not valid JSON: %w", err)
	}
	if parsed.Error != nil {
		return nil, errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("the model returned no choices")
	}

	return &Result{
		Content: parsed.Choices[0].Message.Content,
		Tokens:  parsed.Usage.TotalTokens,
		Model:   model,
	}, nil
}
