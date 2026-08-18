package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Discovering which model to use, from the endpoint itself.
//
// The same standard that defines /chat/completions defines /models, so an
// administrator who has given us an address has already given us everything
// needed to answer "which model?". Asking them to type a name as well is
// asking them to copy a string out of another screen and get it exactly right
// - and the failure when they do not is a 404 from the completions call, which
// says nothing about the actual mistake.
//
// A name that *is* configured always wins. Discovery only fills the gap.

// modelList is the /models response. Servers differ in what else they return
// per entry - owner, created, capabilities - and none of it is needed here.
type modelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Models asks the endpoint what it serves.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, ErrDisabled
	}

	endpoint := strings.TrimSuffix(c.cfg.Endpoint, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the model could not be reached: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the model list answered %s: %s", resp.Status,
			strings.TrimSpace(string(raw)))
	}

	var parsed modelList
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("the model list was not valid JSON: %w", err)
	}

	out := make([]string, 0, len(parsed.Data))
	for _, entry := range parsed.Data {
		if id := strings.TrimSpace(entry.ID); id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

// Models that cannot hold a conversation, whatever else they are good at.
// Sending this prompt to an embedding or speech model is not a degraded
// answer, it is an error - so they are excluded before anything is ranked.
var notChat = regexp.MustCompile(
	`(?i)embed|whisper|tts|audio|speech|dall-?e|image|vision-only|moderation|rerank|clip|` +
		// The legacy completions family, which does not accept a messages array.
		`babbage|davinci|curie|ada-|-instruct$|realtime|transcribe|-search-|similarity`)

// Names that suggest a small, cheap model. Preferred because of what this is
// for: classifying a short request against a fixed list of four priorities and
// a handful of names. That is not work a large model does better - it is the
// same answer at several times the price.
var smallModel = regexp.MustCompile(`(?i)mini|small|lite|tiny|nano|haiku|flash|phi|[^0-9](1|2|3|4|7|8|9)b\b`)

// ErrNoModels is returned when the endpoint lists nothing usable.
var ErrNoModels = errors.New("the endpoint lists no model that can hold a conversation")

// PickModel chooses one id from what an endpoint offers.
//
// Any choice among several is a guess, so the guess is made cheap-first and,
// above all, **stable**: the same list always yields the same model, or a
// suggestion would silently change character between sweeps. Whatever it picks
// is logged, stored on every suggestion it produces, and shown by the test
// button - so an administrator who disagrees can see the choice and pin
// AI_MODEL, which always wins.
func PickModel(ids []string) (string, error) {
	usable := make([]string, 0, len(ids))
	for _, id := range ids {
		if !notChat.MatchString(id) {
			usable = append(usable, id)
		}
	}
	if len(usable) == 0 {
		return "", ErrNoModels
	}
	// One model is the ordinary case for a server on a company's own network,
	// and there is nothing to guess about.
	if len(usable) == 1 {
		return usable[0], nil
	}

	sort.SliceStable(usable, func(i, j int) bool {
		iSmall, jSmall := smallModel.MatchString(usable[i]), smallModel.MatchString(usable[j])
		if iSmall != jSmall {
			return iSmall
		}
		// Alphabetical within a tier. Arbitrary, but arbitrary and repeatable
		// beats arbitrary and different on every process start.
		return usable[i] < usable[j]
	})
	return usable[0], nil
}

// The discovered name, remembered per endpoint.
//
// Outside the client rather than on it, because the sweep builds a fresh
// client every pass - it re-reads the settings so an administrator's change
// takes effect without a restart - and a cache on the client would therefore
// be a cache that never survives to be used. Keyed by endpoint so repointing
// at a different server discovers again rather than reusing a name that server
// may not serve.
var discovery struct {
	sync.Mutex
	byEndpoint map[string]discovered
}

type discovered struct {
	model string
	at    time.Time
}

// Long enough that a sweep every minute does not list models every minute,
// short enough that a model loaded into a local server this afternoon is found
// this afternoon.
const discoveryTTL = 10 * time.Minute

// resolveModel returns the model to send, discovering one if none was
// configured.
func (c *Client) resolveModel(ctx context.Context) (string, error) {
	if c.cfg.Model != "" {
		return c.cfg.Model, nil
	}

	discovery.Lock()
	if entry, ok := discovery.byEndpoint[c.cfg.Endpoint]; ok && time.Since(entry.at) < discoveryTTL {
		discovery.Unlock()
		return entry.model, nil
	}
	discovery.Unlock()

	// Deliberately outside the lock: this is a network call, and holding a
	// package-wide mutex across one would make every caller wait on the
	// slowest endpoint anybody is using. Two callers racing here both discover,
	// agree on the answer, and the second write is harmless.
	ids, err := c.Models(ctx)
	if err != nil {
		return "", err
	}
	model, err := PickModel(ids)
	if err != nil {
		return "", err
	}

	discovery.Lock()
	if discovery.byEndpoint == nil {
		discovery.byEndpoint = map[string]discovered{}
	}
	discovery.byEndpoint[c.cfg.Endpoint] = discovered{model: model, at: time.Now()}
	discovery.Unlock()

	return model, nil
}

// ForgetDiscovered drops what was discovered for an endpoint, so the next call
// asks again. Used when the settings change, since the point of a stored name
// is speed and not memory.
func ForgetDiscovered() {
	discovery.Lock()
	discovery.byEndpoint = nil
	discovery.Unlock()
}
