// Package api exposes the REST surface over chi. WebSocket upgrades
// are delegated to the ws package via the wsHandler parameter so api
// stays independent of hub internals.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"jmessage/internal/auth"
	"jmessage/internal/store"
)

// Presence answers "is this user online" for API responses; the ws.Hub
// implements it. NoPresence serves handler tests.
type Presence interface {
	IsOnline(userID string) bool
}

// NoPresence reports everyone offline.
type NoPresence struct{}

func (NoPresence) IsOnline(string) bool { return false }

// Server carries the dependencies handlers need.
type Server struct {
	Store    *store.Store
	Tokens   *auth.Tokens
	Presence Presence
	Logger   *slog.Logger
}

// Router assembles the full HTTP mux. wsHandler (may be nil) is mounted
// at /ws and does its own token authentication (query parameter).
func (s *Server) Router(wsHandler http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)

	r.Post("/api/register", s.handleRegister)
	r.Post("/api/login", s.handleLogin)

	r.Group(func(r chi.Router) {
		r.Use(s.Tokens.Middleware)
		r.Get("/api/me", s.handleMe)
		r.Get("/api/users/lookup", s.handleUserLookup)
		r.Post("/api/sync", s.handleSync)
		r.Get("/api/conversations", s.handleListConversations)
		r.Post("/api/conversations", s.handleCreateConversation)
		r.Get("/api/conversations/{id}", s.handleGetConversation)
		r.Get("/api/conversations/{id}/members", s.handleListMembers)
		r.Post("/api/conversations/{id}/members", s.handleAddMember)
		r.Get("/api/conversations/{id}/messages", s.handleHistory)
		r.Get("/api/conversations/{id}/receipts", s.handleReceipts)
	})

	if wsHandler != nil {
		r.Handle("/ws", wsHandler)
	}
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "malformed JSON body")
		return false
	}
	return true
}

// storeErr maps store sentinels onto HTTP statuses.
func storeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrUsernameTaken):
		writeErr(w, http.StatusConflict, "username already taken")
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}
