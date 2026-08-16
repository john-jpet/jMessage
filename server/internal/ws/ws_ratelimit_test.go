package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"jmessage/internal/model"
)

// recvAny reads the next non-presence frame (presence broadcasts are
// unordered background noise, same as recv skips them).
func (c *wsClient) recvAny() Frame {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, data, err := c.conn.Read(ctx)
		cancel()
		if err != nil {
			c.t.Fatalf("waiting for a frame: %v", err)
		}
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil {
			c.t.Fatal(err)
		}
		if f.Type == TypePresence {
			continue
		}
		return f
	}
}

// TestSendRateLimit floods a single connection with TypeSend frames
// faster than the per-connection sendLimit bucket (burst 10) allows and
// expects the overflow to come back as a rate_limited error instead of
// being persisted.
func TestSendRateLimit(t *testing.T) {
	e := newWSEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	conv, _ := e.st.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})

	ca := e.dial(alice.ID)
	const n = sendBurst + 5
	for i := 0; i < n; i++ {
		ca.send(Frame{Type: TypeSend, TempID: string(rune('a' + i)), ConvID: conv.ID, Body: "x"})
	}

	acks, limited := 0, 0
	for i := 0; i < n; i++ {
		f := ca.recvAny()
		switch f.Type {
		case TypeAck:
			acks++
		case TypeError:
			if f.Code == "rate_limited" {
				limited++
			}
		}
	}
	if limited == 0 {
		t.Fatalf("got %d acks, %d rate-limit errors out of %d sends; expected some throttled", acks, limited, n)
	}
	if acks > sendBurst {
		t.Fatalf("acks = %d, want <= burst of %d", acks, sendBurst)
	}
}

// TestFrameRateLimit floods a connection with cheap frames (ping) past
// the general per-connection frameLimit bucket. Every ping gets exactly
// one reply — pong if allowed, a rate_limited error otherwise — so the
// two counts are exact.
func TestFrameRateLimit(t *testing.T) {
	e := newWSEnv(t)
	ca := e.dial(e.user("bob").ID)

	const n = frameBurst + 10
	for i := 0; i < n; i++ {
		ca.send(Frame{Type: TypePing})
	}

	pongs, limited := 0, 0
	for i := 0; i < n; i++ {
		f := ca.recvAny()
		switch {
		case f.Type == TypePong:
			pongs++
		case f.Type == TypeError && f.Code == "rate_limited":
			limited++
		}
	}
	if limited == 0 {
		t.Fatalf("got %d pongs, %d rate-limit errors out of %d pings; expected some throttled", pongs, limited, n)
	}
	if pongs > frameBurst {
		t.Fatalf("pongs = %d, want <= burst of %d", pongs, frameBurst)
	}
}
