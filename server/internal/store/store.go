// Package store is the persistence layer: typed repositories over the
// embedded PetDB engine. It owns the key schema (keys.go) and every
// crash-tolerance pattern the engine's no-transactions model requires:
// write ordering, hint+recover counters, and read-time reconciliation.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	petdb "petdb"
)

// Sentinel errors surfaced to the API layer.
var (
	ErrNotFound      = errors.New("store: not found")
	ErrUsernameTaken = errors.New("store: username taken")
	ErrNotMember     = errors.New("store: not a conversation member")
)

// Store wraps the PetDB handle with typed operations. One Store owns
// the data directory for the process lifetime.
type Store struct {
	db     *petdb.DB
	logger *slog.Logger

	regMu  sync.Mutex // serializes username check-then-put
	convMu sync.Mutex // serializes conversation creation (DM dedup)

	users *counter
	convs *counter
	seqs  *seqAllocator
}

// Open opens (or creates) the database at dir and recovers ID counters.
func Open(dir string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	db, err := petdb.Open(dir, &petdb.Options{Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("store: open petdb: %w", err)
	}
	s := &Store{db: db, logger: logger}
	if s.users, err = openCounter(db, []byte("counter:user"), []byte("user:")); err != nil {
		db.Close()
		return nil, err
	}
	if s.convs, err = openCounter(db, []byte("counter:conv"), []byte("conversation:")); err != nil {
		db.Close()
		return nil, err
	}
	s.seqs = newSeqAllocator(db)
	return s, nil
}

// Close releases the engine (flushes what is queued; WAL replays the rest).
func (s *Store) Close() error { return s.db.Close() }

// getJSON loads and unmarshals one record, mapping petdb.ErrNotFound.
func (s *Store) getJSON(key []byte, out any) error {
	v, err := s.db.Get(key)
	if errors.Is(err, petdb.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(v, out)
}

// putJSON marshals and durably writes one record.
func (s *Store) putJSON(key []byte, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Put(key, b)
}
