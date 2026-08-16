// Package api exposes the REST surface over chi. WebSocket upgrades
// are delegated to the ws package via the wsHandler parameter so api
// stays independent of hub internals.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"jmessage/internal/auth"
	"jmessage/internal/blob"
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
	Store          *store.Store
	Blobs          *blob.Store
	Tokens         *auth.Tokens
	Presence       Presence
	Reactions      ReactionNotifier // nil = no live fanout (tests)
	Logger         *slog.Logger
	MaxUploadBytes int64 // 0 = DefaultMaxUploadBytes

	// AllowedOrigins enables CORS for these exact Origin values (e.g.
	// "https://app.example.com"). Empty = no CORS headers, for the
	// same-origin deployment (static assets served by this process).
	AllowedOrigins []string
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			for _, allowed := range s.AllowedOrigins {
				if origin == allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
					break
				}
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Router assembles the full HTTP mux. wsHandler (may be nil) is mounted
// at /ws and does its own token authentication (query parameter). static
// (may be nil) serves the built frontend for every unmatched GET, with
// an index.html fallback for client-side (History API) routes.
func (s *Server) Router(wsHandler http.Handler, static fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	if len(s.AllowedOrigins) > 0 {
		r.Use(s.corsMiddleware)
	}

	// Router-wide backstop: every request, authenticated or not, pays a
	// generous per-IP cap. This is the safety net for routes that do
	// their own auth outside the Bearer group (attachment downloads,
	// /ws) and for anything added later without a bespoke limiter.
	r.Use(newRateLimiter(600, time.Minute).byIPHandler)

	// Credential-stuffing / registration-spam guard: tighter per-IP caps
	// ahead of the Argon2 hashing and store lookups those handlers do.
	loginLimit := newRateLimiter(10, time.Minute)
	registerLimit := newRateLimiter(5, time.Hour)
	r.Post("/api/register", registerLimit.byIP(s.handleRegister))
	r.Post("/api/login", loginLimit.byIP(s.handleLogin))

	// General per-user cap for every authenticated action, plus tighter
	// per-user caps on the endpoints that do real work (storage writes,
	// disk I/O) rather than a cheap read.
	authLimit := newRateLimiter(180, time.Minute)
	uploadLimit := newRateLimiter(30, time.Minute)
	convCreateLimit := newRateLimiter(20, time.Minute)
	reactionLimit := newRateLimiter(60, time.Minute)

	r.Group(func(r chi.Router) {
		r.Use(s.Tokens.Middleware)
		r.Use(authLimit.byUserHandler)
		r.Get("/api/me", s.handleMe)
		r.Get("/api/users/lookup", s.handleUserLookup)
		r.Get("/api/users/{id}/profile", s.handleProfile)
		r.Get("/api/settings/profile", s.handleGetProfileSettings)
		r.Patch("/api/settings/profile", s.handlePatchProfileSettings)
		r.Post("/api/sync", s.handleSync)
		r.Get("/api/conversations", s.handleListConversations)
		r.Post("/api/conversations", convCreateLimit.byUser(s.handleCreateConversation))
		r.Get("/api/conversations/{id}", s.handleGetConversation)
		r.Get("/api/conversations/{id}/members", s.handleListMembers)
		r.Post("/api/conversations/{id}/members", s.handleAddMember)
		r.Get("/api/conversations/{id}/messages", s.handleHistory)
		r.Put("/api/conversations/{id}/messages/{seq}/reactions/{emoji}", reactionLimit.byUser(s.handleReaction(true)))
		r.Delete("/api/conversations/{id}/messages/{seq}/reactions/{emoji}", reactionLimit.byUser(s.handleReaction(false)))
		r.Get("/api/conversations/{id}/receipts", s.handleReceipts)
		r.Post("/api/uploads", uploadLimit.byUser(s.handleUpload))
	})

	// Outside the Bearer-only group: does its own header-or-query token
	// check so <img src> can load attachments. Covered by the router-wide
	// per-IP backstop above.
	r.Get("/api/attachments/{id}", s.handleDownload)

	if wsHandler != nil {
		r.Handle("/ws", wsHandler)
	}

	if static != nil {
		r.NotFound(spaHandler(static))
	}
	return r
}

// spaHandler serves files from static, falling back to index.html for
// any path that isn't a real static asset (client-side routes like
// /settings) so browser refresh and deep links work. The fallback
// writes index.html's bytes directly rather than rewriting the request
// path, since http.FileServer treats a request path ending in
// "/index.html" as a canonicalization case and redirects it to "./".
func spaHandler(static fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(static))
	indexHTML, _ := fs.ReadFile(static, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := static.Open(path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// userMsg strips the validation sentinel prefix for API error bodies.
func userMsg(err error) string {
	return strings.TrimPrefix(err.Error(), "validation: ")
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
