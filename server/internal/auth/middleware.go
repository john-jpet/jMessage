package auth

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey struct{}

// UserID extracts the authenticated user ID from a request context,
// empty if the request was not authenticated.
func UserID(ctx context.Context) string {
	uid, _ := ctx.Value(ctxKey{}).(string)
	return uid
}

// WithUserID returns a context carrying the authenticated user ID
// (exported for the WebSocket upgrade path and tests).
func WithUserID(ctx context.Context, uid string) context.Context {
	return context.WithValue(ctx, ctxKey{}, uid)
}

// Middleware rejects requests without a valid Bearer token and injects
// the user ID into the context for handlers downstream.
func (t *Tokens) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		uid, err := t.Verify(token)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), uid)))
	})
}
