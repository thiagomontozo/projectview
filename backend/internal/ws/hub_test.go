package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newTestHub starts a hub behind an httptest server that authenticates by
// trusting a "user" query parameter - the real handler does the same after
// validating a token, and the token path is covered elsewhere.
func newTestHub(t *testing.T) (*Hub, string) {
	t.Helper()
	hub := NewHub()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWS(w, r, r.URL.Query().Get("user"))
	}))
	t.Cleanup(server.Close)
	return hub, "ws" + strings.TrimPrefix(server.URL, "http")
}

func dial(t *testing.T, url, userID string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url+"?user="+userID, nil)
	if err != nil {
		t.Fatalf("dial as %s: %v", userID, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readMessage returns the next frame, or fails if none arrives in time.
func readMessage(t *testing.T, conn *websocket.Conn) Message {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}
	return msg
}

// Silence is asserted by sending a known frame afterwards and requiring it to
// be the very next thing read: anything the hub emitted in between would show
// up first. Waiting on a read timeout would work once and then leave the
// connection unusable - gorilla treats any read error as terminal.

// waitForOnline gives the hub's register goroutine a moment to run. Dialing
// returns as soon as the handshake completes, which is a hair before the
// connection is in the map.
func waitForOnline(t *testing.T, hub *Hub, userID string, online bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if hub.IsOnline(userID) == online {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("user %s never became online=%v", userID, online)
}

func payloadOf(t *testing.T, msg Message) map[string]any {
	t.Helper()
	data, err := json.Marshal(msg.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return out
}

func TestPresenceAnnouncesOnlyTransitions(t *testing.T) {
	hub, url := newTestHub(t)
	hub.SetNameResolver(func(userID string) string { return "User " + userID })

	observer := dial(t, url, "observer")
	waitForOnline(t, hub, "observer", true)
	readMessage(t, observer) // its own arrival

	dial(t, url, "ana")
	msg := readMessage(t, observer)
	if msg.Type != "presence" {
		t.Fatalf("expected a presence frame, got %q", msg.Type)
	}
	payload := payloadOf(t, msg)
	if payload["userId"] != "ana" || payload["online"] != true {
		t.Fatalf("unexpected presence payload: %v", payload)
	}
	if payload["name"] != "User ana" {
		t.Errorf("presence should carry the display name, got %v", payload["name"])
	}

	// A second tab must not announce anything: someone with two windows open
	// is not online twice, and every client would redraw for nothing. Nor does
	// closing one of two connections take them offline.
	second := dial(t, url, "ana")
	second.Close()
	if !hub.IsOnline("ana") {
		t.Fatal("closing one of two connections must not take the user offline")
	}

	// Bruno's arrival is the next thing the observer should see. A frame for
	// ana's second tab - opening or closing - would arrive before it.
	dial(t, url, "bruno")
	payload = payloadOf(t, readMessage(t, observer))
	if payload["userId"] != "bruno" {
		t.Fatalf("a second tab must not announce presence; got a frame for %v", payload["userId"])
	}
}

func TestPresenceGoesOfflineWithTheLastConnection(t *testing.T) {
	hub, url := newTestHub(t)

	observer := dial(t, url, "observer")
	waitForOnline(t, hub, "observer", true)
	readMessage(t, observer) // its own arrival

	ana := dial(t, url, "ana")
	if readMessage(t, observer).Type != "presence" {
		t.Fatal("expected the online announcement")
	}

	ana.Close()
	msg := readMessage(t, observer)
	payload := payloadOf(t, msg)
	if msg.Type != "presence" || payload["userId"] != "ana" || payload["online"] != false {
		t.Fatalf("expected ana to go offline, got %s %v", msg.Type, payload)
	}
	waitForOnline(t, hub, "ana", false)

	if online := hub.OnlineUsers(); len(online) != 1 || online[0] != "observer" {
		t.Fatalf("only the observer should remain online, got %v", online)
	}
}

func TestTypingAnnouncesOnceAndStops(t *testing.T) {
	hub, url := newTestHub(t)

	observer := dial(t, url, "observer")
	waitForOnline(t, hub, "observer", true)
	readMessage(t, observer) // its own arrival

	ana := dial(t, url, "ana")
	readMessage(t, observer) // ana's presence

	if err := ana.WriteJSON(Inbound{Type: InboundTyping, Channel: "general"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	msg := readMessage(t, observer)
	payload := payloadOf(t, msg)
	if msg.Type != "typing" || payload["typing"] != true || payload["channel"] != "general" {
		t.Fatalf("expected a typing frame for general, got %s %v", msg.Type, payload)
	}

	// A client re-sends "typing" as a keep-alive; rebroadcasting each one would
	// turn one person typing into a flood. The stop frame that follows must be
	// the next thing the observer reads.
	for i := 0; i < 3; i++ {
		if err := ana.WriteJSON(Inbound{Type: InboundTyping, Channel: "general"}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	if typists := hub.TypingIn("general"); len(typists) != 1 || typists[0] != "ana" {
		t.Fatalf("expected ana to be typing in general, got %v", typists)
	}

	if err := ana.WriteJSON(Inbound{Type: InboundStopTyping, Channel: "general"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	msg = readMessage(t, observer)
	if payload := payloadOf(t, msg); msg.Type != "typing" || payload["typing"] != false {
		t.Fatalf("keep-alives must not be rebroadcast; expected the stop frame, got %s %v", msg.Type, payload)
	}
	if typists := hub.TypingIn("general"); len(typists) != 0 {
		t.Fatalf("nobody should be typing, got %v", typists)
	}
}

func TestDisconnectingClearsTyping(t *testing.T) {
	hub, url := newTestHub(t)

	observer := dial(t, url, "observer")
	waitForOnline(t, hub, "observer", true)
	readMessage(t, observer) // its own arrival

	ana := dial(t, url, "ana")
	readMessage(t, observer)

	if err := ana.WriteJSON(Inbound{Type: InboundTyping, Channel: "general"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	readMessage(t, observer)

	// Someone who closes the tab mid-word must not stay "typing" forever.
	ana.Close()
	waitForOnline(t, hub, "ana", false)
	if typists := hub.TypingIn("general"); len(typists) != 0 {
		t.Fatalf("a disconnect must clear the typing entry, got %v", typists)
	}
}

func TestUnreadableFrameDoesNotDropTheConnection(t *testing.T) {
	hub, url := newTestHub(t)

	ana := dial(t, url, "ana")
	waitForOnline(t, hub, "ana", true)
	readMessage(t, ana) // its own arrival

	// A client on an older build sending something we cannot parse should keep
	// working rather than be disconnected.
	if err := ana.WriteMessage(websocket.TextMessage, []byte("not json at all")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ana.WriteJSON(Inbound{Type: InboundTyping, Channel: "general"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	msg := readMessage(t, ana)
	if msg.Type != "typing" {
		t.Fatalf("expected the connection to survive and deliver typing, got %q", msg.Type)
	}
	if !hub.IsOnline("ana") {
		t.Fatal("the connection should still be registered")
	}
}

func TestSendToUserReachesEveryConnection(t *testing.T) {
	hub, url := newTestHub(t)

	first := dial(t, url, "ana")
	waitForOnline(t, hub, "ana", true)
	readMessage(t, first) // its own arrival
	second := dial(t, url, "ana")
	// The second connection is registered without announcing anything, so wait
	// on the hub rather than on a frame.
	for i := 0; i < 100 && len(hub.OnlineUsers()) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	hub.SendToUser("ana", Message{Type: "notification", Payload: map[string]string{"title": "hello"}})

	for _, conn := range []*websocket.Conn{first, second} {
		msg := readMessage(t, conn)
		if msg.Type != "notification" {
			t.Fatalf("every open tab should receive the push, got %q", msg.Type)
		}
	}
}
