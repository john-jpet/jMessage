package ws

import (
	"log/slog"
	"sync"
	"time"

	"jmessage/internal/store"
)

// typingInterval rate-limits typing relays per (user, conversation).
const typingInterval = 2 * time.Second

// Hub tracks online users (userID → connection set, multi-device) and
// fans frames out. Presence is purely in-memory per the spec; only
// message payloads are persisted.
type Hub struct {
	store  *store.Store
	logger *slog.Logger

	mu    sync.RWMutex
	conns map[string]map[*Client]struct{}

	typingMu   sync.Mutex
	lastTyping map[string]time.Time // userID+":"+convID → last relay
}

func NewHub(st *store.Store, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Hub{
		store:      st,
		logger:     logger,
		conns:      make(map[string]map[*Client]struct{}),
		lastTyping: make(map[string]time.Time),
	}
}

// IsOnline satisfies api.Presence.
func (h *Hub) IsOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[userID]) > 0
}

// register adds a connection; a 0→1 transition broadcasts presence.
func (h *Hub) register(c *Client) {
	h.mu.Lock()
	set, ok := h.conns[c.UserID]
	if !ok {
		set = make(map[*Client]struct{})
		h.conns[c.UserID] = set
	}
	set[c] = struct{}{}
	wasOffline := len(set) == 1
	h.mu.Unlock()
	if wasOffline {
		go h.broadcastPresence(c.UserID, true)
	}
}

// unregister removes a connection; a 1→0 transition broadcasts offline.
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	set := h.conns[c.UserID]
	delete(set, c)
	nowOffline := len(set) == 0
	if nowOffline {
		delete(h.conns, c.UserID)
	}
	h.mu.Unlock()
	if nowOffline {
		go h.broadcastPresence(c.UserID, false)
	}
}

// sendToUsers queues a frame on every connection of the given users,
// skipping exclude (the frame's origin connection).
func (h *Hub) sendToUsers(userIDs []string, exclude *Client, frame []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, uid := range userIDs {
		for c := range h.conns[uid] {
			if c != exclude {
				c.trySend(frame)
			}
		}
	}
}

// broadcastPresence tells everyone who shares a conversation with the
// user (and the user's own other devices) about an online transition.
// Runs async off register/unregister; store scans are µs-scale.
func (h *Hub) broadcastPresence(userID string, online bool) {
	convs, err := h.store.ListUserConversations(userID)
	if err != nil {
		h.logger.Warn("presence broadcast: list conversations", "user", userID, "err", err)
		return
	}
	audience := map[string]bool{userID: true}
	for _, conv := range convs {
		members, err := h.store.ListMembers(conv.ID)
		if err != nil {
			continue
		}
		for _, uid := range members {
			audience[uid] = true
		}
	}
	uids := make([]string, 0, len(audience))
	for uid := range audience {
		uids = append(uids, uid)
	}
	h.sendToUsers(uids, nil, encode(Frame{Type: TypePresence, UserID: userID, Online: &online}))
}

// relayTyping forwards a typing event to the conversation's online
// members, rate-limited per (user, conversation).
func (h *Hub) relayTyping(from *Client, convID string) {
	key := from.UserID + ":" + convID
	h.typingMu.Lock()
	now := time.Now()
	if now.Sub(h.lastTyping[key]) < typingInterval {
		h.typingMu.Unlock()
		return
	}
	h.lastTyping[key] = now
	// Opportunistic sweep so the map doesn't grow unboundedly.
	if len(h.lastTyping) > 4096 {
		for k, t := range h.lastTyping {
			if now.Sub(t) > time.Minute {
				delete(h.lastTyping, k)
			}
		}
	}
	h.typingMu.Unlock()

	members, err := h.store.ListMembers(convID)
	if err != nil {
		return
	}
	h.sendToUsers(members, from, encode(Frame{Type: TypeTyping, ConvID: convID, UserID: from.UserID}))
}

// NotifyReaction fans a reaction change to every member's connections —
// no exclusions: the origin client also renders the server's count.
// Implements api.ReactionNotifier for REST-initiated reactions.
func (h *Hub) NotifyReaction(convID string, seq uint64, emoji, userID string, count int, added bool) {
	members, err := h.store.ListMembers(convID)
	if err != nil {
		return
	}
	c := count
	h.sendToUsers(members, nil, encode(Frame{
		Type: TypeReactionUpdate, ConvID: convID, Seq: seq,
		Emoji: emoji, UserID: userID, Count: &c, Added: added,
	}))
}

// CloseAll disconnects every client (used for graceful shutdown).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	var all []*Client
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.mu.Unlock()
	for _, c := range all {
		c.close("server shutting down")
	}
}
