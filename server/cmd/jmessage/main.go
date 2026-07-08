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
	"time"

	"jmessage/internal/api"
	"jmessage/internal/auth"
	"jmessage/internal/store"
	"jmessage/internal/ws"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "./data", "PetDB data directory")
	jwtSecret := flag.String("jwt-secret", "", "JWT signing secret (or env JMESSAGE_JWT_SECRET); empty = random per-process (sessions die on restart)")
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
	apiServer := &api.Server{Store: st, Tokens: tokens, Presence: hub, Logger: logger}

	srv := &http.Server{Addr: *addr, Handler: apiServer.Router(wsHandler)}
	go func() {
		logger.Info("jmessage listening", "addr", *addr, "data", *dataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	logger.Info("shutting down")
	hub.CloseAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	if err := st.Close(); err != nil {
		logger.Error("close store", "err", err)
	}
}
