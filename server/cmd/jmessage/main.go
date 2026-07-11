// jmessage is the application server: REST + WebSocket over an
// embedded PetDB store. One process owns the data directory.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"jmessage/internal/api"
	"jmessage/internal/auth"
	"jmessage/internal/blob"
	"jmessage/internal/store"
	"jmessage/internal/ws"
)

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

	tokens := auth.NewTokens([]byte(secret))
	hub := ws.NewHub(st, logger)
	wsHandler := &ws.Handler{
		Store:  st,
		Tokens: tokens,
		Hub:    hub,
		Logger: logger,
		// Dev: the Vite proxy forwards the browser origin (localhost:5173).
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	}
	apiServer := &api.Server{
		Store:          st,
		Blobs:          blobs,
		Tokens:         tokens,
		Presence:       hub,
		Reactions:      hub,
		Logger:         logger,
		MaxUploadBytes: int64(*maxUploadMB) << 20,
	}

	srv := &http.Server{Addr: *addr, Handler: apiServer.Router(wsHandler)}
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
