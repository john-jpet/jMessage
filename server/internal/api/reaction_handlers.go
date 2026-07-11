package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"jmessage/internal/validation"
)

// ReactionNotifier fans a reaction change out to online conversation
// members; the ws.Hub implements it. Nil is fine (tests).
type ReactionNotifier interface {
	NotifyReaction(convID string, seq uint64, emoji, userID string, count int, added bool)
}

// handleReaction serves PUT (add) and DELETE (remove) on
// /api/conversations/{id}/messages/{seq}/reactions/{emoji}.
func (s *Server) handleReaction(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conv, uid, ok := s.requireMember(w, r)
		if !ok {
			return
		}
		seq, err := strconv.ParseUint(chi.URLParam(r, "seq"), 10, 64)
		if err != nil || seq == 0 {
			writeErr(w, http.StatusBadRequest, "bad message sequence")
			return
		}
		emoji := chi.URLParam(r, "emoji") // chi URL-decodes
		if err := validation.Emoji(emoji); err != nil {
			writeErr(w, http.StatusBadRequest, userMsg(err))
			return
		}

		var count int
		if add {
			count, err = s.Store.AddReaction(conv.ID, seq, emoji, uid)
		} else {
			count, err = s.Store.RemoveReaction(conv.ID, seq, emoji, uid)
		}
		if err != nil {
			storeErr(w, err)
			return
		}
		if s.Reactions != nil {
			s.Reactions.NotifyReaction(conv.ID, seq, emoji, uid, count, add)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"emoji": emoji, "count": count, "reacted": add,
		})
	}
}
