package api

import (
	"net/http"

	"jmessage/internal/auth"
	"jmessage/internal/model"
)

// syncRequest is the client's statement of what it already holds:
// one entry per known conversation with the highest seq stored locally.
type syncRequest struct {
	Conversations []struct {
		ID      string `json:"id"`
		LastSeq uint64 `json:"lastSeq"`
	} `json:"conversations"`
}

// syncChange is one conversation's catch-up payload.
type syncChange struct {
	Conversation convResponse    `json:"conversation"`
	Messages     []model.Message `json:"messages"`
	HasMore      bool            `json:"hasMore"`   // repeat sync (or page history) to continue
	LatestSeq    uint64          `json:"latestSeq"` // server's current tip
}

// handleSync returns everything the caller is missing: new messages in
// the conversations it named, plus entire conversations it has never
// seen (created while it was offline). Non-member entries in the
// request are silently skipped — sync is not an authorization oracle.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var req syncRequest
	if !readJSON(w, r, &req) {
		return
	}

	known := make(map[string]uint64, len(req.Conversations))
	for _, c := range req.Conversations {
		known[c.ID] = c.LastSeq
	}

	// The membership index is the source of truth for what the user
	// should have; anything not in the request syncs from seq 0.
	convs, err := s.Store.ListUserConversations(uid)
	if err != nil {
		storeErr(w, err)
		return
	}

	changes := make([]syncChange, 0, len(convs))
	for _, conv := range convs {
		after := known[conv.ID] // 0 for unknown conversations
		msgs, hasMore, err := s.Store.ListMessagesAfter(conv.ID, after, 0)
		if err != nil {
			storeErr(w, err)
			return
		}
		if err := s.Store.AttachReactions(conv.ID, msgs, uid); err != nil {
			storeErr(w, err)
			return
		}
		latest, err := s.Store.LastSeq(conv.ID)
		if err != nil {
			storeErr(w, err)
			return
		}
		if _, wasKnown := known[conv.ID]; wasKnown && len(msgs) == 0 {
			continue // fully caught up; keep the response small
		}
		if msgs == nil {
			msgs = []model.Message{}
		}
		changes = append(changes, syncChange{
			Conversation: s.decorate(conv, uid),
			Messages:     msgs,
			HasMore:      hasMore,
			LatestSeq:    latest,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changes})
}

// handleReceipts returns every member's read/delivered watermarks for a
// conversation — the hydration source for ✓/✓✓ indicators; live updates
// arrive as WS receipt frames.
func (s *Server) handleReceipts(w http.ResponseWriter, r *http.Request) {
	conv, _, ok := s.requireMember(w, r)
	if !ok {
		return
	}
	members, err := s.Store.ListMembers(conv.ID)
	if err != nil {
		storeErr(w, err)
		return
	}
	states, err := s.Store.ListReadStates(conv.ID, members)
	if err != nil {
		storeErr(w, err)
		return
	}
	type receipt struct {
		UserID       string `json:"userID"`
		DeliveredSeq uint64 `json:"deliveredSeq"`
		ReadSeq      uint64 `json:"readSeq"`
	}
	out := make([]receipt, 0, len(states))
	for uid, rs := range states {
		out = append(out, receipt{UserID: uid, DeliveredSeq: rs.DeliveredSeq, ReadSeq: rs.ReadSeq})
	}
	writeJSON(w, http.StatusOK, out)
}
