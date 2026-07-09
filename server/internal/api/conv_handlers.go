package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"jmessage/internal/auth"
	"jmessage/internal/model"
)

// convResponse decorates conversation metadata with per-viewer fields.
type convResponse struct {
	model.Conversation
	MemberCount int    `json:"memberCount"`
	PeerID       string `json:"peerID,omitempty"`       // dm only
	PeerName     string `json:"peerName,omitempty"`     // dm only
	PeerOnline   bool   `json:"peerOnline,omitempty"`   // dm only
	PeerLastSeen int64  `json:"peerLastSeen,omitempty"` // dm only, unix ms
	DisplayName string `json:"displayName,omitempty"` // list-friendly title
	LastSeq     uint64 `json:"lastSeq"`               // conversation tip
	MyReadSeq   uint64 `json:"myReadSeq"`             // viewer's read watermark
	Unread      uint64 `json:"unread"`                // lastSeq - myReadSeq
}

// requireMember loads the conversation and verifies the caller belongs
// to it, writing the error response on failure.
func (s *Server) requireMember(w http.ResponseWriter, r *http.Request) (*model.Conversation, string, bool) {
	uid := auth.UserID(r.Context())
	cid := chi.URLParam(r, "id")
	conv, err := s.Store.GetConversation(cid)
	if err != nil {
		storeErr(w, err)
		return nil, "", false
	}
	ok, err := s.Store.IsMember(cid, uid)
	if err != nil {
		storeErr(w, err)
		return nil, "", false
	}
	if !ok {
		writeErr(w, http.StatusForbidden, "not a member of this conversation")
		return nil, "", false
	}
	return conv, uid, true
}

// decorate fills viewer-relative fields (DM peer name/presence, title,
// unread count from the viewer's read watermark).
func (s *Server) decorate(conv model.Conversation, viewerID string) convResponse {
	out := convResponse{Conversation: conv, DisplayName: conv.Name}
	members, err := s.Store.ListMembers(conv.ID)
	if err == nil {
		out.MemberCount = len(members)
	}
	if conv.Type == model.ConvDM {
		for _, uid := range members {
			if uid != viewerID {
				out.PeerID = uid
				if peer, err := s.Store.GetUser(uid); err == nil {
					out.PeerName = peer.DisplayName
					out.DisplayName = peer.DisplayName
				}
				out.PeerOnline = s.Presence.IsOnline(uid)
				if last, err := s.Store.LastSeen(uid); err == nil {
					out.PeerLastSeen = last
				}
			}
		}
	}
	if last, err := s.Store.LastSeq(conv.ID); err == nil {
		out.LastSeq = last
		if rs, err := s.Store.GetReadState(viewerID, conv.ID); err == nil {
			out.MyReadSeq = rs.ReadSeq
			if last > rs.ReadSeq {
				out.Unread = last - rs.ReadSeq
			}
		}
	}
	return out
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	convs, err := s.Store.ListUserConversations(uid)
	if err != nil {
		storeErr(w, err)
		return
	}
	out := make([]convResponse, 0, len(convs))
	for _, c := range convs {
		out = append(out, s.decorate(c, uid))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	var req struct {
		Type           string   `json:"type"`
		Name           string   `json:"name"`
		ParticipantIDs []string `json:"participantIDs"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Type == model.ConvGroup && req.Name == "" {
		writeErr(w, http.StatusBadRequest, "group conversations need a name")
		return
	}
	for _, pid := range req.ParticipantIDs {
		if _, err := s.Store.GetUser(pid); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown participant "+pid)
			return
		}
	}
	conv, err := s.Store.CreateConversation(req.Type, req.Name, uid, req.ParticipantIDs)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.decorate(*conv, uid))
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	conv, uid, ok := s.requireMember(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.decorate(*conv, uid))
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	conv, _, ok := s.requireMember(w, r)
	if !ok {
		return
	}
	uids, err := s.Store.ListMembers(conv.ID)
	if err != nil {
		storeErr(w, err)
		return
	}
	out := make([]model.UserSummary, 0, len(uids))
	for _, uid := range uids {
		u, err := s.Store.GetUser(uid)
		if err != nil {
			continue
		}
		sum := u.Summary()
		sum.Online = s.Presence.IsOnline(uid)
		out = append(out, sum)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	conv, _, ok := s.requireMember(w, r)
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"userID"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.Store.AddMember(conv.ID, req.UserID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	conv, _, ok := s.requireMember(w, r)
	if !ok {
		return
	}
	before, _ := strconv.ParseUint(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	msgs, hasMore, err := s.Store.ListMessagesBefore(conv.ID, before, limit)
	if err != nil {
		storeErr(w, err)
		return
	}
	if msgs == nil {
		msgs = []model.Message{}
	}
	oldest := uint64(0)
	if len(msgs) > 0 {
		oldest = msgs[0].Seq
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": msgs,
		"hasMore":  hasMore,
		"oldestSeq": oldest,
	})
}
