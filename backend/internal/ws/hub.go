// Package ws implements the WebSocket hub used for realtime chat,
// notifications, presence and typing indicators.
//
// The connection used to be push-only: the server told clients what changed
// and clients wrote back over REST. That is still true for anything that
// persists — creating a task, sending a message — because REST gives those
// writes validation, authorization and an audit trail a socket frame would
// have to reimplement.
//
// What the socket now also carries is *ephemeral* state: who is online, and
// who is typing where. Neither is worth a database row or an HTTP request:
// both are obsolete within seconds, and losing them on a reconnect costs
// nothing.
package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"projectview/internal/logger"
)

// Message is the envelope pushed to clients. Type is one of:
// "notification", "chat:message", "chat:reaction", "presence", "typing".
type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// Inbound is what a client may send. Deliberately tiny: anything that changes
// stored state goes through the REST API instead.
type Inbound struct {
	Type string `json:"type"`
	// Channel scopes a typing indicator.
	Channel string `json:"channel,omitempty"`
}

const (
	InboundTyping     = "typing"
	InboundStopTyping = "typing:stop"
	InboundPing       = "ping"
)

type client struct {
	userID string
	conn   *websocket.Conn
	send   chan Message
}

// Hub tracks every open connection, grouped by authenticated user id, so a
// message can be pushed to "all of this user's open tabs".
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*client]bool

	// typing[channelID][userID] = when it was last reported. A timestamp
	// rather than a boolean, because a client that closes its laptop mid-word
	// never sends the stop frame; entries simply expire.
	typingMu sync.Mutex
	typing   map[string]map[string]time.Time

	// Resolves a user id to a display name for presence payloads, so clients
	// do not have to hold the whole directory to render "Ana is typing".
	nameOf func(userID string) string
}

const typingTTL = 6 * time.Second

func NewHub() *Hub {
	hub := &Hub{
		clients: make(map[string]map[*client]bool),
		typing:  make(map[string]map[string]time.Time),
	}
	go hub.expireTyping()
	return hub
}

// SetNameResolver supplies the lookup used to label presence and typing
// events. Optional: without it, payloads carry ids only.
func (h *Hub) SetNameResolver(resolve func(userID string) string) {
	h.nameOf = resolve
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // CORS is handled at the HTTP layer
}

// ServeWS upgrades the connection and registers it under userID until it
// disconnects. userID must already have been authenticated by the caller.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, userID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("websocket upgrade failed: %v", err)
		return
	}

	c := &client{userID: userID, conn: conn, send: make(chan Message, 32)}
	h.register(c)

	go h.writePump(c)
	h.readPump(c) // blocks until the connection closes
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	first := len(h.clients[c.userID]) == 0
	if h.clients[c.userID] == nil {
		h.clients[c.userID] = make(map[*client]bool)
	}
	h.clients[c.userID][c] = true
	h.mu.Unlock()

	// Only the first connection changes presence: a second tab does not make
	// someone twice as online.
	if first {
		h.broadcastPresence(c.userID, true)
	}
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	last := false
	if conns, ok := h.clients[c.userID]; ok {
		delete(conns, c)
		if len(conns) == 0 {
			delete(h.clients, c.userID)
			last = true
		}
	}
	// Typing is dropped while the client map is still locked, so going offline
	// and stopping typing become one indivisible change. Doing it after the
	// unlock left a window in which the hub answered "not online" and "still
	// typing" at the same time - long enough for a caller that reacts to the
	// presence event to read the contradiction, and the reason the disconnect
	// test failed intermittently.
	var cleared []string
	if last {
		cleared = h.takeTypingFor(c.userID)
	}
	h.mu.Unlock()
	close(c.send)

	// Announced outside both locks: each of these broadcasts, and broadcasting
	// takes h.mu again.
	if last {
		h.broadcastPresence(c.userID, false)
		for _, channelID := range cleared {
			h.announceTyping(channelID, c.userID, false)
		}
	}
}

func (h *Hub) readPump(c *client) {
	defer func() {
		h.unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(4096)
	c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var inbound Inbound
		if err := json.Unmarshal(data, &inbound); err != nil {
			// A frame we cannot parse is not worth dropping the connection
			// for; a client on an older build should keep working.
			continue
		}

		switch inbound.Type {
		case InboundTyping:
			h.setTyping(inbound.Channel, c.userID, true)
		case InboundStopTyping:
			h.setTyping(inbound.Channel, c.userID, false)
		case InboundPing:
			// Keeps the read deadline alive on networks that swallow pongs.
			c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		}
	}
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				logger.Error("websocket marshal failed: %v", err)
				continue
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// SendToUser pushes a message to every open connection for a given user id.
func (h *Hub) SendToUser(userID string, msg Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients[userID] {
		select {
		case c.send <- msg:
		default:
			// A client too slow to drain its buffer is dropped from this
			// message rather than allowed to block every other recipient.
			logger.Warn("dropping websocket message for user %s: send buffer full", userID)
		}
	}
}

// SendToUsers pushes a message to several users at once (e.g. every member
// of a chat channel).
func (h *Hub) SendToUsers(userIDs []string, msg Message) {
	for _, id := range userIDs {
		h.SendToUser(id, msg)
	}
}

// Broadcast pushes to every connected user. Used for presence, which is not
// scoped to a channel.
func (h *Hub) Broadcast(msg Message) {
	h.mu.RLock()
	recipients := make([]string, 0, len(h.clients))
	for userID := range h.clients {
		recipients = append(recipients, userID)
	}
	h.mu.RUnlock()

	for _, userID := range recipients {
		h.SendToUser(userID, msg)
	}
}

// OnlineUsers lists who currently has at least one connection open.
func (h *Hub) OnlineUsers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.clients))
	for userID := range h.clients {
		out = append(out, userID)
	}
	return out
}

// IsOnline reports whether a user has any open connection.
func (h *Hub) IsOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

type presencePayload struct {
	UserID string `json:"userId"`
	Name   string `json:"name,omitempty"`
	Online bool   `json:"online"`
}

func (h *Hub) broadcastPresence(userID string, online bool) {
	payload := presencePayload{UserID: userID, Online: online}
	if h.nameOf != nil {
		payload.Name = h.nameOf(userID)
	}
	h.Broadcast(Message{Type: "presence", Payload: payload})
}

// ---------------------------------------------------------------------------
// Typing indicators
// ---------------------------------------------------------------------------

type typingPayload struct {
	Channel string `json:"channel"`
	UserID  string `json:"userId"`
	Name    string `json:"name,omitempty"`
	Typing  bool   `json:"typing"`
}

func (h *Hub) setTyping(channelID, userID string, typing bool) {
	if channelID == "" {
		return
	}

	if typing {
		h.typingMu.Lock()
		if h.typing[channelID] == nil {
			h.typing[channelID] = make(map[string]time.Time)
		}
		_, wasTyping := h.typing[channelID][userID]
		h.typing[channelID][userID] = time.Now()
		h.typingMu.Unlock()

		// Only announce the transition. A client re-sends "typing" on every
		// keystroke, and rebroadcasting each one would turn one person typing
		// into a flood.
		if wasTyping {
			return
		}
	} else {
		h.typingMu.Lock()
		if h.typing[channelID] != nil {
			delete(h.typing[channelID], userID)
		}
		h.typingMu.Unlock()
	}

	h.announceTyping(channelID, userID, typing)
}

func (h *Hub) announceTyping(channelID, userID string, typing bool) {
	payload := typingPayload{Channel: channelID, UserID: userID, Typing: typing}
	if h.nameOf != nil {
		payload.Name = h.nameOf(userID)
	}
	// Broadcast rather than scoped to the channel's members: the hub does not
	// know channel membership, and the client ignores events for channels it
	// is not looking at. The payload carries no message content.
	h.Broadcast(Message{Type: "typing", Payload: payload})
}

// TypingIn lists who is currently typing in a channel, excluding entries that
// have gone stale.
func (h *Hub) TypingIn(channelID string) []string {
	h.typingMu.Lock()
	defer h.typingMu.Unlock()

	out := []string{}
	cutoff := time.Now().Add(-typingTTL)
	for userID, at := range h.typing[channelID] {
		if at.After(cutoff) {
			out = append(out, userID)
		}
	}
	return out
}

// expireTyping clears indicators nobody stopped explicitly — a closed laptop,
// a lost connection, a tab switched away mid-word.
func (h *Hub) expireTyping() {
	ticker := time.NewTicker(typingTTL / 2)
	defer ticker.Stop()

	for range ticker.C {
		expired := map[string][]string{}

		h.typingMu.Lock()
		cutoff := time.Now().Add(-typingTTL)
		for channelID, users := range h.typing {
			for userID, at := range users {
				if at.Before(cutoff) {
					delete(users, userID)
					expired[channelID] = append(expired[channelID], userID)
				}
			}
			if len(users) == 0 {
				delete(h.typing, channelID)
			}
		}
		h.typingMu.Unlock()

		for channelID, users := range expired {
			for _, userID := range users {
				h.announceTyping(channelID, userID, false)
			}
		}
	}
}

// takeTypingFor removes every typing entry a user holds and returns the
// channels they were in. It only mutates - the caller announces - because it is
// called with h.mu held, and announcing takes h.mu again.
//
// The lock order is h.mu then typingMu, and nothing takes them the other way
// round: every path that holds typingMu releases it before broadcasting.
func (h *Hub) takeTypingFor(userID string) []string {
	cleared := []string{}

	h.typingMu.Lock()
	defer h.typingMu.Unlock()

	for channelID, users := range h.typing {
		if _, ok := users[userID]; ok {
			delete(users, userID)
			cleared = append(cleared, channelID)
		}
		if len(users) == 0 {
			delete(h.typing, channelID)
		}
	}
	return cleared
}
