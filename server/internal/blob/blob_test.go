package blob

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pngHeader is enough magic for content sniffing.
var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestSaveRoundtrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := append(append([]byte{}, pngHeader...), bytes.Repeat([]byte{0xAB}, 4096)...)
	wantSum := sha256.Sum256(payload)

	res, err := s.Save(bytes.NewReader(payload), "application/wrong-hint")
	if err != nil {
		t.Fatal(err)
	}
	if res.Size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", res.Size, len(payload))
	}
	if res.SHA256 != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("sha mismatch")
	}
	if res.MimeType != "image/png" {
		t.Fatalf("mime = %q, want image/png (sniff beats client hint)", res.MimeType)
	}

	f, err := s.Open(res.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(got, payload) {
		t.Fatal("bytes differ after roundtrip")
	}

	// tmp/ is empty after a successful save.
	ents, _ := os.ReadDir(filepath.Join(s.dir, "tmp"))
	if len(ents) != 0 {
		t.Fatalf("tmp not empty: %d entries", len(ents))
	}
}

func TestClientMimeFallback(t *testing.T) {
	s, _ := Open(t.TempDir())
	// Random bytes sniff as octet-stream → client hint wins.
	res, err := s.Save(bytes.NewReader(bytes.Repeat([]byte{0x01, 0xFE}, 300)), "application/x-custom")
	if err != nil {
		t.Fatal(err)
	}
	if res.MimeType != "application/x-custom" {
		t.Fatalf("mime = %q", res.MimeType)
	}
}

func TestOpenRejectsTraversal(t *testing.T) {
	s, _ := Open(t.TempDir())
	for _, id := range []string{"../secret", "..\\secret", "abc", "a8f92c1f-7d3e-4e21-a44c-9f3d1a2b7c8Z"} {
		if _, err := s.Open(id); !os.IsNotExist(err) {
			t.Fatalf("Open(%q) err = %v, want not-exist", id, err)
		}
	}
}

func TestListAndSweepTmp(t *testing.T) {
	s, _ := Open(t.TempDir())
	res, err := s.Save(bytes.NewReader([]byte("hello world contents")), "")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil || len(entries) != 1 || entries[0].ID != res.ID {
		t.Fatalf("List = %+v, %v", entries, err)
	}

	// A crashed upload lingers in tmp until the sweep.
	stalePath := filepath.Join(s.dir, "tmp", NewID())
	os.WriteFile(stalePath, []byte("partial"), 0o644)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(stalePath, old, old)

	n, err := s.SweepTmp(24 * time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("SweepTmp = %d, %v", n, err)
	}

	if err := s.Remove(res.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(res.ID); err != nil {
		t.Fatal("Remove not idempotent:", err)
	}
	entries, _ = s.List()
	if len(entries) != 0 {
		t.Fatalf("blob survived Remove: %+v", entries)
	}
}
