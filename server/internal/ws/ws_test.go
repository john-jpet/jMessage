package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"jmessage/internal/auth"
	"jmessage/internal/model"
	"jmessage/internal/store"
)

type wsEnv struct {
	t      *testing.T
	st     *store.Store
	tokens *auth.Tokens
	hub    *Hub
	srv    *httptest.Server
}

func newWSEnv(t *testing.T) *wsEnv {
	t.Helper()
	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tokens := auth.NewTokens([]byte("ws-test-secret"))
	hub := NewHub(st, nil)
	h := &Handler{Store: st, Tokens: tokens, Hub: hub, OriginPatterns: []string{"*"}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &wsEnv{t: t, st: st, tokens: tokens, hub: hub, srv: srv}
}

func (e *wsEnv) user(name string) *model.User {
	e.t.Helper()
	u, err := e.st.CreateUser(name, name, "hash")
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func (e *wsEnv) dial(uid string) *wsClient {
	e.t.Helper()
	tok, _ := e.tokens.Mint(uid)
	url := strings.Replace(e.srv.URL, "http://", "ws://", 1) + "?token=" + tok
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return &wsClient{t: e.t, conn: conn}
}

func (c *wsClient) send(f Frame) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, encode(f)); err != nil {
		c.t.Fatal(err)
	}
}

// recv reads frames until one matches the wanted type (skipping
// unrelated presence noise), failing after the timeout.
func (c *wsClient) recv(wantType string) Frame {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, data, err := c.conn.Read(ctx)
		cancel()
		if err != nil {
			c.t.Fatalf("waiting for %q frame: %v", wantType, err)
		}
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil {
			c.t.Fatal(err)
		}
		if f.Type == wantType {
			return f
		}
		if f.Type == TypePresence {
			continue // unordered background noise
		}
		c.t.Fatalf("got %q frame while waiting for %q: %+v", f.Type, wantType, f)
	}
}

// expectNone asserts no frame of the given type arrives within 300ms.
func (c *wsClient) expectNone(frameType string) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return // timeout: good
		}
		var f Frame
		json.Unmarshal(data, &f)
		if f.Type == frameType {
			c.t.Fatalf("unexpected %q frame: %+v", frameType, f)
		}
	}
}

func TestSendAckAndDelivery(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})

	ca := e.dial(alice.ID)
	cb := e.dial(bob.ID)
	ca2 := e.dial(alice.ID) // alice's second device

	ca.send(Frame{Type: TypeSend, TempID: "t-1", ConvID: conv.ID, Body: "hello bob"})

	ack := ca.recv(TypeAck)
	if ack.TempID != "t-1" || ack.Seq != 1 {
		t.Fatalf("ack = %+v", ack)
	}
	msg := cb.recv(TypeMessage)
	if msg.Body != "hello bob" || msg.SenderID != alice.ID || msg.Seq != 1 {
		t.Fatalf("delivered = %+v", msg)
	}
	// Sender's other device gets the message too; the sending connection
	// gets only the ack.
	dev2 := ca2.recv(TypeMessage)
	if dev2.Seq != 1 {
		t.Fatalf("second device = %+v", dev2)
	}
	ca.expectNone(TypeMessage)

	// Message durably persisted with correct seq.
	msgs, _, err := e.st.ListMessagesBefore(conv.ID, 0, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Body != "hello bob" {
		t.Fatalf("persisted = %+v, %v", msgs, err)
	}
}

func TestNonMemberRejected(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	mallory := e.user("mallory")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})

	cm := e.dial(mallory.ID)
	cm.send(Frame{Type: TypeSend, TempID: "t-x", ConvID: conv.ID, Body: "let me in"})
	errFrame := cm.recv(TypeError)
	if errFrame.Code != "not_member" || errFrame.TempID != "t-x" {
		t.Fatalf("error = %+v", errFrame)
	}
	// Nothing persisted.
	msgs, _, _ := e.st.ListMessagesBefore(conv.ID, 0, 10)
	if len(msgs) != 0 {
		t.Fatalf("non-member message persisted: %+v", msgs)
	}
}

func TestTypingRelayedNotPersisted(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})

	ca, cb := e.dial(alice.ID), e.dial(bob.ID)
	ca.send(Frame{Type: TypeTyping, ConvID: conv.ID})
	typ := cb.recv(TypeTyping)
	if typ.UserID != alice.ID || typ.ConvID != conv.ID {
		t.Fatalf("typing = %+v", typ)
	}
	// Rate limit: an immediate second event is swallowed.
	ca.send(Frame{Type: TypeTyping, ConvID: conv.ID})
	cb.expectNone(TypeTyping)

	msgs, _, _ := e.st.ListMessagesBefore(conv.ID, 0, 10)
	if len(msgs) != 0 {
		t.Fatal("typing event persisted")
	}
}

func TestPresenceTransitions(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})

	cb := e.dial(bob.ID)
	if !waitOnline(e.hub, bob.ID, true) {
		t.Fatal("bob not online after dial")
	}

	// Alice connects: bob sees her come online.
	ca := e.dial(alice.ID)
	on := cb.recvPresence(alice.ID)
	if on.Online == nil || !*on.Online {
		t.Fatalf("presence(online) = %+v", on)
	}

	// Alice disconnects entirely: bob sees offline.
	ca.conn.Close(websocket.StatusNormalClosure, "bye")
	off := cb.recvPresence(alice.ID)
	if off.Online == nil || *off.Online {
		t.Fatalf("presence(offline) = %+v", off)
	}
	if !waitOnline(e.hub, alice.ID, false) {
		t.Fatal("alice still online after close")
	}
}

// recvPresence waits for a presence frame about the given user.
func (c *wsClient) recvPresence(uid string) Frame {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, data, err := c.conn.Read(ctx)
		cancel()
		if err != nil {
			c.t.Fatalf("waiting for presence(%s): %v", uid, err)
		}
		var f Frame
		json.Unmarshal(data, &f)
		if f.Type == TypePresence && f.UserID == uid {
			return f
		}
	}
}

func waitOnline(h *Hub, uid string, want bool) bool {
	for i := 0; i < 100; i++ {
		if h.IsOnline(uid) == want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestBadTokenRejected(t *testing.T) {
	e := newWSEnv(t)
	url := strings.Replace(e.srv.URL, "http://", "ws://", 1) + "?token=garbage"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, url, nil)
	if err == nil {
		t.Fatal("garbage token accepted")
	}
	if resp != nil && resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestConcurrentSendersOverWS(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})

	const clients, per = 4, 10
	done := make(chan error, clients)
	for i := 0; i < clients; i++ {
		c := e.dial(alice.ID)
		go func(c *wsClient, i int) {
			for j := 0; j < per; j++ {
				c.send(Frame{Type: TypeSend, TempID: fmt.Sprintf("c%d-%d", i, j), ConvID: conv.ID, Body: "x"})
			}
			// Each connection receives its own acks plus other devices'
			// messages; count acks only.
			acks := 0
			deadline := time.Now().Add(10 * time.Second)
			for acks < per {
				ctx, cancel := context.WithDeadline(context.Background(), deadline)
				_, data, err := c.conn.Read(ctx)
				cancel()
				if err != nil {
					done <- fmt.Errorf("client %d: %w", i, err)
					return
				}
				var f Frame
				json.Unmarshal(data, &f)
				if f.Type == TypeAck {
					acks++
				}
			}
			done <- nil
		}(c, i)
	}
	for i := 0; i < clients; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}

	last, _ := e.st.LastSeq(conv.ID)
	if last != clients*per {
		t.Fatalf("LastSeq = %d, want %d (dense under concurrent WS senders)", last, clients*per)
	}
}
