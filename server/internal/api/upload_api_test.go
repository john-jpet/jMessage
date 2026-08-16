package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"jmessage/internal/model"
)

var pngPayload = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{0x7F}, 2048)...)

// upload posts raw bytes and returns (status, ref).
func (e *testEnv) upload(token, filename, mime string, body []byte) (int, model.AttachmentRef) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/uploads", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Filename", filename)
	if mime != "" {
		req.Header.Set("Content-Type", mime)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var ref model.AttachmentRef
	json.NewDecoder(resp.Body).Decode(&ref)
	return resp.StatusCode, ref
}

// download GETs attachment bytes; token may ride the header or query.
func (e *testEnv) download(id, token string, viaQuery bool) (int, []byte, string) {
	e.t.Helper()
	url := e.srv.URL + "/api/attachments/" + id
	if viaQuery {
		url += "?token=" + token
	}
	req, _ := http.NewRequest("GET", url, nil)
	if !viaQuery {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, resp.Header.Get("Content-Type")
}

func TestUploadDownloadAuthorization(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	bTok, bUser := e.register("bob")
	cTok, _ := e.register("carol")

	// Alice uploads.
	code, ref := e.upload(aTok, "photo.png", "", pngPayload)
	if code != http.StatusOK || ref.ID == "" || ref.MimeType != "image/png" || ref.Size != int64(len(pngPayload)) {
		t.Fatalf("upload: %d %+v", code, ref)
	}
	if ref.Filename != "photo.png" {
		t.Fatalf("filename = %q", ref.Filename)
	}
	att, err := e.store.GetAttachment(ref.ID)
	if err != nil || att.State != model.AttachmentPending || att.OwnerID != aUser.ID {
		t.Fatalf("metadata: %+v, %v", att, err)
	}

	// Pending: owner may download, others may not.
	if code, body, mime := e.download(ref.ID, aTok, false); code != http.StatusOK || !bytes.Equal(body, pngPayload) || mime != "image/png" {
		t.Fatalf("owner download: %d mime=%s len=%d", code, mime, len(body))
	}
	if code, _, _ := e.download(ref.ID, bTok, false); code != http.StatusForbidden {
		t.Fatalf("pending non-owner status %d", code)
	}

	// Commit it into an alice↔bob DM.
	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	if _, err := e.store.AppendMessage(dm.ID, aUser.ID, "cm-up", "", []string{ref.ID}); err != nil {
		t.Fatal(err)
	}

	// Member bob downloads (query-token path too); non-member carol 403.
	if code, body, _ := e.download(ref.ID, bTok, false); code != http.StatusOK || !bytes.Equal(body, pngPayload) {
		t.Fatalf("member download: %d", code)
	}
	// Cache poisoning guard: authorization-dependent responses must be
	// private and vary on the Authorization header.
	{
		req, _ := http.NewRequest("GET", e.srv.URL+"/api/attachments/"+ref.ID, nil)
		req.Header.Set("Authorization", "Bearer "+bTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=3600" {
			t.Fatalf("Cache-Control = %q", cc)
		}
		if v := resp.Header.Get("Vary"); v != "Authorization" {
			t.Fatalf("Vary = %q", v)
		}
	}
	if code, _, _ := e.download(ref.ID, bTok, true); code != http.StatusOK {
		t.Fatalf("query-token download: %d", code)
	}
	if code, _, _ := e.download(ref.ID, cTok, false); code != http.StatusForbidden {
		t.Fatalf("non-member download: %d", code)
	}

	// Bad token / unknown id.
	if code, _, _ := e.download(ref.ID, "garbage", false); code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", code)
	}
	if code, _, _ := e.download("a8f92c1f-7d3e-4e21-a44c-9f3d1a2b7c88", aTok, false); code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", code)
	}
}

func TestUploadLimits(t *testing.T) {
	e := newEnv(t)
	aTok, _ := e.register("alice")

	// Over the 1 MiB test cap → 413.
	if code, _ := e.upload(aTok, "big.bin", "application/octet-stream", bytes.Repeat([]byte{1}, (1<<20)+512)); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload: %d", code)
	}
	// Empty body → 400.
	if code, _ := e.upload(aTok, "empty.bin", "", nil); code != http.StatusBadRequest {
		t.Fatalf("empty upload: %d", code)
	}
	// Unauthenticated → 401.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/uploads", bytes.NewReader([]byte("x")))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon upload: %d", resp.StatusCode)
	}
	// Traversal filename is reduced to its base name.
	code, ref := e.upload(aTok, "..\\..\\windows\\system32\\evil.png", "", pngPayload)
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	if ref.Filename != "evil.png" {
		t.Fatalf("filename not sanitized: %q", ref.Filename)
	}

	// No leftover garbage: exactly the blobs whose uploads succeeded.
	entries, _ := e.blobs.List()
	if len(entries) != 1 {
		t.Fatalf("blob count = %d, want 1", len(entries))
	}
}

// TestUploadMimeRestriction: attachments are limited to images/GIFs/video
// (sniffed content, not the client-declared header) — no PDFs, archives,
// or other general files.
func TestUploadMimeRestriction(t *testing.T) {
	e := newEnv(t)
	aTok, _ := e.register("alice")

	if code, _ := e.upload(aTok, "photo.png", "", pngPayload); code != http.StatusOK {
		t.Fatalf("png upload: %d", code)
	}
	// Sniffing wins over a spoofed Content-Type header.
	if code, _ := e.upload(aTok, "fake.png", "image/png", []byte("not actually a png")); code != http.StatusBadRequest {
		t.Fatalf("spoofed mime upload: %d", code)
	}
	if code, _ := e.upload(aTok, "doc.pdf", "application/pdf", []byte("%PDF-1.4 not a real pdf but declared as one")); code != http.StatusBadRequest {
		t.Fatalf("pdf upload: %d", code)
	}
	if code, _ := e.upload(aTok, "archive.zip", "application/zip", []byte("PK\x03\x04 fake zip bytes")); code != http.StatusBadRequest {
		t.Fatalf("zip upload: %d", code)
	}
	// A rejected upload leaves no blob behind.
	entries, _ := e.blobs.List()
	if len(entries) != 1 {
		t.Fatalf("blob count = %d, want 1 (only the accepted png)", len(entries))
	}
}

// TestUploadRateLimit: uploads are the most storage/CPU-costly
// authenticated action, so they carry their own tighter per-user cap
// (30/min) on top of the general per-user request cap.
func TestUploadRateLimit(t *testing.T) {
	e := newEnv(t)
	aTok, _ := e.register("alice")

	var last int
	for i := 0; i < 31; i++ {
		last, _ = e.upload(aTok, "x.png", "", pngPayload)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("31st upload in a minute: %d, want 429", last)
	}
}

// TestMessagesCarryAttachmentRefs: history and sync responses embed the
// refs with zero extra work.
func TestMessagesCarryAttachmentRefs(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	bTok, bUser := e.register("bob")

	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{bUser.ID}}, &dm)
	_, ref := e.upload(aTok, "photo.png", "", testPNG(t, 32, 32))
	if _, err := e.store.AppendMessage(dm.ID, aUser.ID, "cm-1", "see attached", []string{ref.ID}); err != nil {
		t.Fatal(err)
	}

	var page struct {
		Messages []model.Message `json:"messages"`
	}
	e.call("GET", "/api/conversations/"+dm.ID+"/messages", bTok, nil, &page)
	if len(page.Messages) != 1 || len(page.Messages[0].Attachments) != 1 ||
		page.Messages[0].Attachments[0].ID != ref.ID {
		t.Fatalf("history refs = %+v", page.Messages)
	}

	var sync syncTestPage
	e.call("POST", "/api/sync", bTok, map[string]any{"conversations": []any{}}, &sync)
	if len(sync.Changes) != 1 || len(sync.Changes[0].Messages[0].Attachments) != 1 {
		t.Fatalf("sync refs = %+v", sync.Changes)
	}
}
