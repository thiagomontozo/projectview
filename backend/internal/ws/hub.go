// Package ws implements a small WebSocket hub used to push chat messages
// and notifications to connected clients in real time. Unlike the original
// Socket.IO-based design, the client always writes (creates tasks, sends
// chat messages, ...) via the REST API; the WebSocket connection is a
// one-way push channel the server uses to notify already-open browser tabs,
// which keeps the protocol trivial to implement and reason about.
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
// "notification", "chat:message".
type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type client struct {
	userID string
	conn   *websocket.Conn
	send   chan Message
}

// Hub tracks every open connection, grouped by authenticated user id, so a
// message can be pushed to "all of this user's open tabs/devices".
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*client]bool
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*client]bool)}
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
	defer h.mu.Unlock()
	if h.clients[c.userID] == nil {
		h.clients[c.userID] = make(map[*client]bool)
	}
	h.clients[c.userID][c] = true
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conns, ok := h.clients[c.userID]; ok {
		delete(conns, c)
		if len(conns) == 0 {
			delete(h.clients, c.userID)
		}
	}
	close(c.send)
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
		// The client never needs to send app-level data; we just drain
		// whatever arrives (ping/pong keeps the deadline alive) until close.
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
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
