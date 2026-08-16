package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/coder/websocket"

	"jmessage/internal/auth"
	"jmessage/internal/model"
	"jmessage/internal/store"
	"jmessage/internal/validation"
)

const (
	outboundBuffer = 256              // sized to absorb a resume replay burst
	readTimeout    = 60 * time.Second // any frame (incl. app ping) resets it
	pingInterval   = 30 * time.Second
	writeTimeout   = 10 * time.Second

	// resumeReplayCap bounds the per-conversation gap the resume fast
	// path will replay inline. Larger gaps are skipped here — the
	// client's POST /api/sync is the authoritative catch-up.
	resumeReplayCap = 50

	// Per-connection frame budgets: generous bursts for read/typing
	// traffic, a tighter sustained rate for handleSend since it does a
	// durable write plus fanout per frame. Far above anything a real
	// client (or human) generates, but enough to stop a flood.
	frameBurst      = 40
	frameRefillRate = 20 // frames/sec sustained, any type
	sendBurst       = 10
	sendRefillRate  = 3 // messages/sec sustained
)

// Handler upgrades /ws?token= requests and runs clients against the hub.
type Handler struct {
	Store          *store.Store
	Tokens         *auth.Tokens
	Hub            *Hub
	Logger         *slog.Logger
	OriginPatterns []string // e.g. ["localhost:*"] for dev behind the Vite proxy
}

// deviceRE validates client-supplied device IDs before they land in a
// PetDB key.
var deviceRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Browsers cannot set headers on WebSocket dials, so the token rides
	// the query string (MVP tradeoff: may appear in access logs).
	uid, err := h.Tokens.Verify(r.URL.Query().Get("token"))
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	deviceID := r.URL.Query().Get("device")
	if !deviceRE.MatchString(deviceID) {
		deviceID = "" // unregistered connection; fanout still works
	}
	if deviceID != "" {
		name := r.Header.Get("User-Agent")
		if len(name) > 80 {
			name = name[:80]
		}
		if err := h.Store.UpsertDevice(uid, deviceID, name); err != nil {
			h.Logger.Warn("device upsert failed", "user", uid, "err", err)
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.OriginPatterns,
	})
	if err != nil {
		return // Accept already replied
	}
	conn.SetReadLimit(64 << 10)

	c := &Client{
		UserID:     uid,
		DeviceID:   deviceID,
		conn:       conn,
		out:        make(chan []byte, outboundBuffer),
		hub:        h.Hub,
		store:      h.Store,
		logger:     h.Logger,
		done:       make(chan struct{}),
		frameLimit: newTokenBucket(frameBurst, frameRefillRate),
		sendLimit:  newTokenBucket(sendBurst, sendRefillRate),
	}
	h.Hub.register(c)
	defer func() {
		h.Hub.unregister(c)
		if deviceID != "" {
			h.Store.TouchDevice(uid, deviceID)
		}
	}()
	c.run(r.Context())
}

// Client is one WebSocket connection of one authenticated user.
type Client struct {
	UserID   string
	DeviceID string

	conn   *websocket.Conn
	out    chan []byte
	hub    *Hub
	store  *store.Store
	logger *slog.Logger

	done      chan struct{}
	closeOnce sync.Once

	frameLimit *tokenBucket // every dispatched frame
	sendLimit  *tokenBucket // TypeSend specifically (persists + fans out)
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
	if f.Type != "" && !c.frameLimit.allow() {
		c.trySend(errorFrame("rate_limited", "too many frames, slow down", f.TempID))
		return
	}
	switch f.Type {
	case TypeSend:
		c.handleSend(f)
	case TypeTyping:
		if ok, err := c.store.IsMember(f.ConvID, c.UserID); err == nil && ok {
			c.hub.relayTyping(c, f.ConvID)
		}
	case TypeResume:
		c.handleResume(f)
	case TypeRead:
		c.handleWatermark(f, true)
	case TypeDelivered:
		c.handleWatermark(f, false)
	case TypeReactionAdd:
		c.handleReaction(f, true)
	case TypeReactionRemove:
		c.handleReaction(f, false)
	case TypePing:
		c.trySend(encode(Frame{Type: TypePong}))
	case "":
		// tolerated bad frame
	default:
		c.trySend(errorFrame("bad_frame", "unknown frame type "+f.Type, f.TempID))
	}
}

// handleResume replays small per-conversation gaps as regular message
// frames. The connection is already registered live (register precedes
// the read loop), so a concurrent send can arrive twice — never be
// missed; the client dedups by (convID, seq). Gaps larger than
// resumeReplayCap are left to the client's REST sync.
func (c *Client) handleResume(f Frame) {
	for cid, lastSeen := range f.LastSeen {
		if ok, err := c.store.IsMember(cid, c.UserID); err != nil || !ok {
			continue
		}
		msgs, hasMore, err := c.store.ListMessagesAfter(cid, lastSeen, resumeReplayCap)
		if err != nil || hasMore {
			continue // REST sync covers big gaps
		}
		for _, m := range msgs {
			c.trySend(encode(Frame{
				Type: TypeMessage, ConvID: m.ConvID, Seq: m.Seq,
				SenderID: m.SenderID, Body: m.Body, TS: m.TS,
				Attachments: m.Attachments,
			}))
		}
	}
}

// handleReaction toggles an emoji reaction and fans the authoritative
// count to every member (including the originating connection — the
// server owns the truth, clients render what they're told).
func (c *Client) handleReaction(f Frame, add bool) {
	if err := validation.Emoji(f.Emoji); err != nil {
		c.trySend(errorFrame("bad_reaction", err.Error(), ""))
		return
	}
	if ok, err := c.store.IsMember(f.ConvID, c.UserID); err != nil || !ok {
		c.trySend(errorFrame("not_member", "not a member of this conversation", ""))
		return
	}
	var (
		count int
		err   error
	)
	if add {
		count, err = c.store.AddReaction(f.ConvID, f.Seq, f.Emoji, c.UserID)
	} else {
		count, err = c.store.RemoveReaction(f.ConvID, f.Seq, f.Emoji, c.UserID)
	}
	if err != nil {
		c.trySend(errorFrame("bad_reaction", "message not found", ""))
		return
	}
	c.hub.NotifyReaction(f.ConvID, f.Seq, f.Emoji, c.UserID, count, add)
}

// handleWatermark raises the sender's read or delivered watermark and,
// on change, fans a receipt to the conversation's online members —
// including the user's own other devices (cross-device unread sync),
// excluding the originating connection.
func (c *Client) handleWatermark(f Frame, read bool) {
	if ok, err := c.store.IsMember(f.ConvID, c.UserID); err != nil || !ok {
		return
	}
	var (
		rs      = model.ReadState{}
		changed bool
		err     error
	)
	if read {
		rs, changed, err = c.store.SetRead(c.UserID, f.ConvID, f.Seq)
	} else {
		rs, changed, err = c.store.SetDelivered(c.UserID, f.ConvID, f.Seq)
	}
	if err != nil || !changed {
		return
	}
	members, err := c.store.ListMembers(f.ConvID)
	if err != nil {
		return
	}
	c.hub.sendToUsers(members, c, encode(Frame{
		Type: TypeReceipt, ConvID: f.ConvID, UserID: c.UserID,
		DeliveredSeq: rs.DeliveredSeq, ReadSeq: rs.ReadSeq,
	}))
}

// handleSend is the persisted-then-acked path: membership check →
// durable AppendMessage → ack the sending connection → fan out to the
// conversation's online members (the sender's other devices included,
// this connection excluded).
func (c *Client) handleSend(f Frame) {
	if !c.sendLimit.allow() {
		c.trySend(errorFrame("rate_limited", "sending too fast, slow down", f.TempID))
		return
	}
	// Centralized rules: ≤2000 chars, valid UTF-8, empty allowed only
	// with attachments.
	if err := validation.MessageBody(f.Body, len(f.AttachmentIDs) > 0); err != nil {
		c.trySend(errorFrame("invalid_message", err.Error(), f.TempID))
		return
	}
	if len(f.AttachmentIDs) > maxAttachments {
		c.trySend(errorFrame("too_many_attachments", "too many attachments", f.TempID))
		return
	}
	if len(f.TempID) > 64 { // it becomes part of a PetDB key
		c.trySend(errorFrame("bad_frame", "tempID too long", ""))
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

	// tempID doubles as the durable idempotency key: a retried frame
	// (lost ack, reconnect replay) acks the original seq, never a dup.
	msg, err := c.store.AppendMessage(f.ConvID, c.UserID, f.TempID, f.Body, f.AttachmentIDs)
	switch {
	case errors.Is(err, store.ErrBadAttachment):
		c.trySend(errorFrame("bad_attachment", "attachment missing or already used", f.TempID))
		return
	case errors.Is(err, store.ErrNotAttachmentOwner):
		c.trySend(errorFrame("not_owner", "attachment belongs to another user", f.TempID))
		return
	case err != nil:
		c.logger.Error("append message", "conv", f.ConvID, "err", err)
		c.trySend(errorFrame("internal", "message could not be persisted", f.TempID))
		return
	}

	c.trySend(encode(Frame{
		Type: TypeAck, TempID: f.TempID, ConvID: msg.ConvID, Seq: msg.Seq, TS: msg.TS,
		Attachments: msg.Attachments,
	}))

	members, err := c.store.ListMembers(msg.ConvID)
	if err != nil {
		return
	}
	c.hub.sendToUsers(members, c, encode(Frame{
		Type: TypeMessage, ConvID: msg.ConvID, Seq: msg.Seq,
		SenderID: msg.SenderID, Body: msg.Body, TS: msg.TS,
		Attachments: msg.Attachments,
	}))
}
