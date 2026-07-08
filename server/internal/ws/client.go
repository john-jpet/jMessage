package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"jmessage/internal/auth"
	"jmessage/internal/store"
)

const (
	outboundBuffer = 64
	readTimeout    = 60 * time.Second // any frame (incl. app ping) resets it
	pingInterval   = 30 * time.Second
	writeTimeout   = 10 * time.Second
)

// Handler upgrades /ws?token= requests and runs clients against the hub.
type Handler struct {
	Store          *store.Store
	Tokens         *auth.Tokens
	Hub            *Hub
	Logger         *slog.Logger
	OriginPatterns []string // e.g. ["localhost:*"] for dev behind the Vite proxy
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Browsers cannot set headers on WebSocket dials, so the token rides
	// the query string (MVP tradeoff: may appear in access logs).
	uid, err := h.Tokens.Verify(r.URL.Query().Get("token"))
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.OriginPatterns,
	})
	if err != nil {
		return // Accept already replied
	}
	conn.SetReadLimit(64 << 10)

	c := &Client{
		UserID: uid,
		conn:   conn,
		out:    make(chan []byte, outboundBuffer),
		hub:    h.Hub,
		store:  h.Store,
		logger: h.Logger,
		done:   make(chan struct{}),
	}
	h.Hub.register(c)
	defer h.Hub.unregister(c)
	c.run(r.Context())
}

// Client is one WebSocket connection of one authenticated user.
type Client struct {
	UserID string

	conn   *websocket.Conn
	out    chan []byte
	hub    *Hub
	store  *store.Store
	logger *slog.Logger

	done      chan struct{}
	closeOnce sync.Once
}

// trySend queues a frame without blocking. A slow client whose buffer
// is full gets disconnected: REST history is the source of truth and
// the frontend refetches on reconnect, so dropping the connection is
// safer than stalling the hub.
func (c *Client) trySend(frame []byte) {
	select {
	case c.out <- frame:
	default:
		c.close("send buffer overflow")
	}
}

func (c *Client) close(reason string) {
	c.closeOnce.Do(func() {
		close(c.done)
		c.conn.Close(websocket.StatusGoingAway, reason)
	})
}

// run drives the write pump in the background and reads until the
// connection dies.
func (c *Client) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go c.writeLoop(ctx)

	for {
		frame, err := c.read(ctx)
		if err != nil {
			c.close("read error")
			return
		}
		c.dispatch(frame)
	}
}

func (c *Client) read(ctx context.Context) (Frame, error) {
	rctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	var f Frame
	_, data, err := c.conn.Read(rctx)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		c.trySend(errorFrame("bad_frame", "malformed frame", ""))
		return Frame{Type: ""}, nil // tolerate one bad frame
	}
	return f, nil
}

func (c *Client) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case frame := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				c.close("write error")
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pctx)
			cancel()
			if err != nil {
				c.close("ping timeout")
				return
			}
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) dispatch(f Frame) {
	switch f.Type {
	case TypeSend:
		c.handleSend(f)
	case TypeTyping:
		if ok, err := c.store.IsMember(f.ConvID, c.UserID); err == nil && ok {
			c.hub.relayTyping(c, f.ConvID)
		}
	case TypePing:
		c.trySend(encode(Frame{Type: TypePong}))
	case "":
		// tolerated bad frame
	default:
		c.trySend(errorFrame("bad_frame", "unknown frame type "+f.Type, f.TempID))
	}
}

// handleSend is the persisted-then-acked path: membership check →
// durable AppendMessage → ack the sending connection → fan out to the
// conversation's online members (the sender's other devices included,
// this connection excluded).
func (c *Client) handleSend(f Frame) {
	if f.Body == "" || len(f.Body) > maxBodyBytes {
		c.trySend(errorFrame("too_large", "message body empty or too large", f.TempID))
		return
	}
	ok, err := c.store.IsMember(f.ConvID, c.UserID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		c.trySend(errorFrame("internal", "membership check failed", f.TempID))
		return
	}
	if !ok {
		c.trySend(errorFrame("not_member", "not a member of this conversation", f.TempID))
		return
	}

	msg, err := c.store.AppendMessage(f.ConvID, c.UserID, f.Body)
	if err != nil {
		c.logger.Error("append message", "conv", f.ConvID, "err", err)
		c.trySend(errorFrame("internal", "message could not be persisted", f.TempID))
		return
	}

	c.trySend(encode(Frame{
		Type: TypeAck, TempID: f.TempID, ConvID: msg.ConvID, Seq: msg.Seq, TS: msg.TS,
	}))

	members, err := c.store.ListMembers(msg.ConvID)
	if err != nil {
		return
	}
	c.hub.sendToUsers(members, c, encode(Frame{
		Type: TypeMessage, ConvID: msg.ConvID, Seq: msg.Seq,
		SenderID: msg.SenderID, Body: msg.Body, TS: msg.TS,
	}))
}
