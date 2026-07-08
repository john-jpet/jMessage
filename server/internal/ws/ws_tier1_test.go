package ws

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"jmessage/internal/model"
)

// dialDevice is dial with a device query parameter.
func (e *wsEnv) dialDevice(uid, deviceID string) *wsClient {
	e.t.Helper()
	tok, _ := e.tokens.Mint(uid)
	url := strings.Replace(e.srv.URL, "http://", "ws://", 1) + "?token=" + tok + "&device=" + deviceID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return &wsClient{t: e.t, conn: conn}
}

// TestDuplicateSendDedup retries a send frame with the same tempID and
// expects the identical seq back with nothing new persisted.
func TestDuplicateSendDedup(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})

	ca := e.dial(alice.ID)
	ca.send(Frame{Type: TypeSend, TempID: "retry-1", ConvID: conv.ID, Body: "once"})
	ack1 := ca.recv(TypeAck)

	// Simulated lost ack: the client sends the exact frame again.
	ca.send(Frame{Type: TypeSend, TempID: "retry-1", ConvID: conv.ID, Body: "once"})
	ack2 := ca.recv(TypeAck)

	if ack1.Seq != ack2.Seq || ack2.Seq != 1 {
		t.Fatalf("acks differ: %d vs %d", ack1.Seq, ack2.Seq)
	}
	msgs, _, _ := e.st.ListMessagesAfter(conv.ID, 0, 0)
	if len(msgs) != 1 {
		t.Fatalf("persisted %d messages, want 1", len(msgs))
	}
}

// TestResumeReplay reconnects with a lastSeen map and expects the gap
// replayed as message frames.
func TestResumeReplay(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})
	for i := 0; i < 5; i++ {
		if _, err := e.st.AppendMessage(conv.ID, alice.ID, "", "m"); err != nil {
			t.Fatal(err)
		}
	}

	cb := e.dial(bob.ID)
	cb.send(Frame{Type: TypeResume, LastSeen: map[string]uint64{conv.ID: 2}})
	for want := uint64(3); want <= 5; want++ {
		m := cb.recv(TypeMessage)
		if m.Seq != want {
			t.Fatalf("replayed seq %d, want %d", m.Seq, want)
		}
	}
	// Nothing beyond the gap.
	cb.expectNone(TypeMessage)

	// A non-member's resume replays nothing.
	mallory := e.user("mallory")
	cm := e.dial(mallory.ID)
	cm.send(Frame{Type: TypeResume, LastSeen: map[string]uint64{conv.ID: 0}})
	cm.expectNone(TypeMessage)
}

// TestReceiptBroadcast: bob reads; alice (and bob's other device) get a
// receipt; regressions are not re-broadcast.
func TestReceiptBroadcast(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})
	for i := 0; i < 3; i++ {
		e.st.AppendMessage(conv.ID, alice.ID, "", "m")
	}

	ca := e.dial(alice.ID)
	cb := e.dial(bob.ID)
	cb2 := e.dial(bob.ID) // bob's second device

	cb.send(Frame{Type: TypeRead, ConvID: conv.ID, Seq: 3})

	r := ca.recv(TypeReceipt)
	if r.UserID != bob.ID || r.ReadSeq != 3 || r.DeliveredSeq != 3 {
		t.Fatalf("receipt = %+v", r)
	}
	// Bob's other device hears it too (cross-device unread sync).
	r2 := cb2.recv(TypeReceipt)
	if r2.UserID != bob.ID || r2.ReadSeq != 3 {
		t.Fatalf("second device receipt = %+v", r2)
	}

	// A regression (seq 1 < 3) changes nothing and broadcasts nothing.
	cb.send(Frame{Type: TypeRead, ConvID: conv.ID, Seq: 1})
	ca.expectNone(TypeReceipt)

	// Watermark persisted.
	rs, _ := e.st.GetReadState(bob.ID, conv.ID)
	if rs.ReadSeq != 3 {
		t.Fatalf("persisted watermark = %+v", rs)
	}
}

// TestDeliveredWatermark: delivered runs ahead of read independently.
func TestDeliveredWatermark(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})
	for i := 0; i < 2; i++ {
		e.st.AppendMessage(conv.ID, alice.ID, "", "m")
	}

	ca := e.dial(alice.ID)
	cb := e.dial(bob.ID)
	cb.send(Frame{Type: TypeDelivered, ConvID: conv.ID, Seq: 2})

	r := ca.recv(TypeReceipt)
	if r.UserID != bob.ID || r.DeliveredSeq != 2 || r.ReadSeq != 0 {
		t.Fatalf("delivered receipt = %+v", r)
	}
}

func TestDeviceRegistration(t *testing.T) {
	e := newWSEnv(t)
	alice := e.user("alice")

	c := e.dialDevice(alice.ID, "dev-test-1")
	devs, err := e.st.ListDevices(alice.ID)
	if err != nil || len(devs) != 1 || devs[0].ID != "dev-test-1" {
		t.Fatalf("devices = %+v, %v", devs, err)
	}

	// Invalid device IDs are ignored, not fatal.
	c2 := e.dialDevice(alice.ID, "bad%3Aid%21")
	_ = c2
	devs, _ = e.st.ListDevices(alice.ID)
	if len(devs) != 1 {
		t.Fatalf("invalid device registered: %+v", devs)
	}
	_ = c
}
