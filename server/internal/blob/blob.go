// Package blob is the attachment byte store: a flat directory of
// immutable files named by random UUID. PetDB never sees these bytes —
// it holds only attachment metadata — which keeps SSTables and the
// block cache free of large values.
//
// Layout:
//
//	<dir>/<uuid>.bin   finalized blobs (immutable)
//	<dir>/tmp/<uuid>   in-flight uploads (atomically renamed on success)
package blob

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// idRE validates blob IDs before they touch the filesystem: UUIDs only,
// no path traversal possible.
var idRE = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

// Store is a directory of immutable blobs.
type Store struct {
	dir string
}

// Open prepares the blob directory (and its tmp/ staging area).
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o755); err != nil {
		return nil, fmt.Errorf("blob: mkdir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// NewID generates a random UUIDv4.
func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// SaveResult describes a finalized blob.
type SaveResult struct {
	ID       string
	Size     int64
	SHA256   string
	MimeType string
}

// Save streams r into a new blob: write to tmp/ while hashing and
// sniffing the content type, fsync, then atomically rename into place.
// A failure at any point removes the temp file — no partial blobs are
// ever visible under their final name. clientMime is used only when
// sniffing is inconclusive.
func (s *Store) Save(r io.Reader, clientMime string) (*SaveResult, error) {
	id := NewID()
	tmpPath := filepath.Join(s.dir, "tmp", id)
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("blob: create temp: %w", err)
	}
	cleanup := func() {
		f.Close()
		os.Remove(tmpPath)
	}

	// Sniff from the first 512 bytes while they pass through.
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		cleanup()
		return nil, err
	}
	head = head[:n]

	hasher := sha256.New()
	w := io.MultiWriter(f, hasher)
	if _, err := w.Write(head); err != nil {
		cleanup()
		return nil, err
	}
	rest, err := io.Copy(w, r)
	if err != nil {
		cleanup()
		return nil, err // includes http.MaxBytesReader overflow
	}
	// Durable before the metadata record references it.
	if err := f.Sync(); err != nil {
		cleanup()
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Rename(tmpPath, s.Path(id)); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("blob: finalize: %w", err)
	}

	mime := http.DetectContentType(head)
	if mime == "application/octet-stream" && clientMime != "" {
		mime = clientMime
	}
	return &SaveResult{
		ID:       id,
		Size:     int64(n) + rest,
		SHA256:   hex.EncodeToString(hasher.Sum(nil)),
		MimeType: mime,
	}, nil
}

// Path returns the final on-disk location for a blob ID. Callers must
// have validated the ID (Open/Remove do).
func (s *Store) Path(id string) string {
	return filepath.Join(s.dir, id+".bin")
}

// Open returns the blob file for reading (supports ServeContent ranges).
func (s *Store) Open(id string) (*os.File, error) {
	if !idRE.MatchString(id) {
		return nil, os.ErrNotExist
	}
	return os.Open(s.Path(id))
}

// Remove deletes a blob (idempotent: missing files are not an error).
func (s *Store) Remove(id string) error {
	if !idRE.MatchString(id) {
		return nil
	}
	err := os.Remove(s.Path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Entry is one finalized blob on disk.
type Entry struct {
	ID      string
	ModTime time.Time
}

// List enumerates finalized blobs (for the orphan sweep).
func (s *Store) List() ([]Entry, error) {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".bin" {
			continue
		}
		id := name[:len(name)-len(".bin")]
		if !idRE.MatchString(id) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{ID: id, ModTime: info.ModTime()})
	}
	return out, nil
}

// SweepTmp removes in-flight upload files older than maxAge (crashed or
// abandoned uploads that never reached the rename).
func (s *Store) SweepTmp(maxAge time.Duration) (int, error) {
	tmpDir := filepath.Join(s.dir, "tmp")
	ents, err := os.ReadDir(tmpDir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, e := range ents {
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(tmpDir, e.Name())) == nil {
			removed++
		}
	}
	return removed, nil
}
