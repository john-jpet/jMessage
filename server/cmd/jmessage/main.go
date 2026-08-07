// jmessage is the application server: REST + WebSocket over an
// embedded PetDB store. One process owns the data directory.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"jmessage/internal/api"
	"jmessage/internal/auth"
	"jmessage/internal/blob"
	"jmessage/internal/store"
	"jmessage/internal/webui"
	"jmessage/internal/ws"
)

// splitList parses a comma-separated flag/env value into trimmed,
// non-empty entries.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// sweepAttachments reclaims attachment garbage: uploads whose message
// never arrived (Pending > 24h; blob first, then metadata — a crash
// between leaves a stale record the next sweep retries), finalized
// blobs with no metadata record (crash before CreateAttachment; the 1h
// age guard covers uploads racing this sweep), and crashed temp files.
func sweepAttachments(st *store.Store, blobs *blob.Store, logger *slog.Logger) {
	stale, err := st.ListStalePendingAttachments(24 * time.Hour)
	if err != nil {
		logger.Warn("stale attachment scan failed", "err", err)
	}
	for _, a := range stale {
		if err := blobs.Remove(a.ID); err != nil {
			logger.Warn("blob remove failed", "id", a.ID, "err", err)
			continue
		}
		if err := st.DeleteAttachment(a.ID); err != nil {
			logger.Warn("attachment delete failed", "id", a.ID, "err", err)
		}
	}
	if len(stale) > 0 {
		logger.Info("pruned abandoned uploads", "count", len(stale))
	}

	entries, err := blobs.List()
	if err != nil {
		logger.Warn("blob list failed", "err", err)
		return
	}
	orphans := 0
	for _, e := range entries {
		if time.Since(e.ModTime) < time.Hour {
			continue
		}
		exists, err := st.AttachmentExists(e.ID)
		if err != nil || exists {
			continue
		}
		if blobs.Remove(e.ID) == nil {
			orphans++
		}
	}
	if orphans > 0 {
		logger.Info("removed orphan blobs", "count", orphans)
	}

	if n, err := blobs.SweepTmp(24 * time.Hour); err == nil && n > 0 {
		logger.Info("swept temp uploads", "count", n)
	}

	// Avatar replacements reclaim the old attachment inline; this covers
	// the crash window between claiming a new avatar and the user-doc
	// update.
	avatarOrphans, err := st.ListOrphanAvatarAttachments(time.Hour)
	if err != nil {
		logger.Warn("avatar orphan scan failed", "err", err)
	}
	for _, a := range avatarOrphans {
		if blobs.Remove(a.ID) == nil {
			st.DeleteAttachment(a.ID)
		}
	}
	if len(avatarOrphans) > 0 {
		logger.Info("pruned orphan avatars", "count", len(avatarOrphans))
	}
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "./data", "PetDB data directory")
	jwtSecret := flag.String("jwt-secret", "", "JWT signing secret (or env JMESSAGE_JWT_SECRET); empty = random per-process (sessions die on restart)")
	maxUploadMB := flag.Int("max-upload-mb", 10, "maximum attachment size in MiB")
	wsOrigins := flag.String("ws-origins", "", "comma-separated WebSocket origin patterns (or env JMESSAGE_WS_ORIGINS); empty = dev default (localhost only)")
	corsOrigins := flag.String("cors-origins", "", "comma-separated allowed CORS origins for cross-origin frontends (or env JMESSAGE_CORS_ORIGINS); empty = no CORS (same-origin deployment)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	secret := *jwtSecret
	if secret == "" {
		secret = os.Getenv("JMESSAGE_JWT_SECRET")
	}
	if secret == "" {
		buf := make([]byte, 32)
		rand.Read(buf)
		secret = hex.EncodeToString(buf)
		logger.Warn("using a random per-process JWT secret; sessions will not survive restarts")
	}

	st, err := store.Open(*dataDir, logger)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	// Attachment bytes live beside (never inside) PetDB. Safe: PetDB
	// only scans its own data/ and wal/ subdirectories.
	blobs, err := blob.Open(filepath.Join(*dataDir, "blobs"))
	if err != nil {
		logger.Error("open blob store", "err", err)
		os.Exit(1)
	}

	origins := splitList(*wsOrigins)
	if origins == nil {
		origins = splitList(os.Getenv("JMESSAGE_WS_ORIGINS"))
	}
	if origins == nil {
		origins = []string{"localhost:*", "127.0.0.1:*"}
		logger.Warn("no -ws-origins configured; allowing only localhost (set -ws-origins or JMESSAGE_WS_ORIGINS for production)")
	}

	cors := splitList(*corsOrigins)
	if cors == nil {
		cors = splitList(os.Getenv("JMESSAGE_CORS_ORIGINS"))
	}

	tokens := auth.NewTokens([]byte(secret))
	hub := ws.NewHub(st, logger)
	wsHandler := &ws.Handler{
		Store:          st,
		Tokens:         tokens,
		Hub:            hub,
		Logger:         logger,
		OriginPatterns: origins,
	}
	apiServer := &api.Server{
		Store:          st,
		Blobs:          blobs,
		Tokens:         tokens,
		Presence:       hub,
		Reactions:      hub,
		Logger:         logger,
		MaxUploadBytes: int64(*maxUploadMB) << 20,
		AllowedOrigins: cors,
	}

	// Serve the embedded frontend only if it was actually built in
	// (dist/ holds a real index.html, not just the source .gitkeep
	// placeholder) — keeps `go run` working API-only against the Vite
	// dev server when no frontend has been embedded.
	var static fs.FS
	if sub, err := fs.Sub(webui.Dist, "dist"); err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			static = sub
			logger.Info("serving embedded frontend")
		}
	}

	srv := &http.Server{Addr: *addr, Handler: apiServer.Router(wsHandler, static)}
	go func() {
		logger.Info("jmessage listening", "addr", *addr, "data", *dataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	// Janitor (hourly): stale idempotency entries, abandoned uploads,
	// orphan blobs, and crashed temp files.
	janitorStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if n, err := st.PruneClientMsgs(7 * 24 * time.Hour); err != nil {
					logger.Warn("clientmsg prune failed", "err", err)
				} else if n > 0 {
					logger.Info("pruned clientmsg entries", "count", n)
				}
				sweepAttachments(st, blobs, logger)
			case <-janitorStop:
				return
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	logger.Info("shutting down")
	close(janitorStop)
	hub.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	if err := st.Close(); err != nil {
		logger.Error("close store", "err", err)
	}
}
