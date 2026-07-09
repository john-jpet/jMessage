package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"jmessage/internal/auth"
	"jmessage/internal/model"
)

// profileResponse is the lightweight profile panel payload.
type profileResponse struct {
	User                model.UserSummary `json:"user"`
	CreatedAt           int64             `json:"createdAt"`
	LastSeen            int64             `json:"lastSeen"`
	SharedConversations []convResponse    `json:"sharedConversations"`
}

// handleProfile returns a user's public profile. Membership-safe: the
// shared-conversation list is computed from the *viewer's* conversations
// filtered by the target's membership, so it can never reveal a
// conversation the viewer doesn't already belong to.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserID(r.Context())
	targetID := chi.URLParam(r, "id")

	target, err := s.Store.GetUser(targetID)
	if err != nil {
		storeErr(w, err)
		return
	}

	sum := target.Summary()
	sum.Online = s.Presence.IsOnline(targetID)
	lastSeen, err := s.Store.LastSeen(targetID)
	if err != nil {
		storeErr(w, err)
		return
	}

	viewerConvs, err := s.Store.ListUserConversations(viewerID)
	if err != nil {
		storeErr(w, err)
		return
	}
	shared := make([]convResponse, 0, 4)
	for _, conv := range viewerConvs {
		if targetID == viewerID {
			break // own profile: skip the shared list, it's everything
		}
		ok, err := s.Store.IsMember(conv.ID, targetID)
		if err != nil {
			storeErr(w, err)
			return
		}
		if ok {
			shared = append(shared, s.decorate(conv, viewerID))
		}
	}

	writeJSON(w, http.StatusOK, profileResponse{
		User:                sum,
		CreatedAt:           target.CreatedAt,
		LastSeen:            lastSeen,
		SharedConversations: shared,
	})
}
