package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"

	"jmessage/internal/model"
)

// testPNG renders a real PNG so image.DecodeConfig works.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x7F
	}
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProfileSettings(t *testing.T) {
	e := newEnv(t)
	aTok, aUser := e.register("alice")
	bTok, _ := e.register("bob")

	// GET returns the raw (empty) display name even though summaries
	// fall back to the username.
	var p settingsProfile
	if code := e.call("GET", "/api/settings/profile", aTok, nil, &p); code != http.StatusOK {
		t.Fatalf("get status %d", code)
	}
	if p.Username != "alice" || p.DisplayName != "" || p.CreatedAt == 0 {
		t.Fatalf("profile = %+v", p)
	}

	// PATCH display name + status.
	if code := e.call("PATCH", "/api/settings/profile", aTok,
		map[string]any{"displayName": "Alice W.", "status": "shipping tier 4 🚀"}, &p); code != http.StatusOK {
		t.Fatalf("patch status %d", code)
	}
	if p.DisplayName != "Alice W." || p.Status != "shipping tier 4 🚀" {
		t.Fatalf("after patch: %+v", p)
	}
	// Absent fields unchanged; empty display name clears (falls back).
	e.call("PATCH", "/api/settings/profile", aTok, map[string]any{"displayName": ""}, &p)
	if p.DisplayName != "" || p.Status != "shipping tier 4 🚀" {
		t.Fatalf("clear semantics: %+v", p)
	}
	var me model.UserSummary
	e.call("GET", "/api/me", aTok, nil, &me)
	if me.DisplayName != "alice" {
		t.Fatalf("summary fallback = %q", me.DisplayName)
	}

	// Validation failures.
	longStatus := make([]byte, 0, 141*4)
	for i := 0; i < 141; i++ {
		longStatus = append(longStatus, 'x')
	}
	if code := e.call("PATCH", "/api/settings/profile", aTok,
		map[string]any{"status": string(longStatus)}, nil); code != http.StatusBadRequest {
		t.Fatalf("141-char status accepted: %d", code)
	}

	// --- Avatar flow ---
	_, ref := e.upload(aTok, "me.png", "", testPNG(t, 128, 128))
	if code := e.call("PATCH", "/api/settings/profile", aTok,
		map[string]any{"avatarAttachmentID": ref.ID}, &p); code != http.StatusOK {
		t.Fatalf("avatar patch status %d", code)
	}
	if p.AvatarID != ref.ID {
		t.Fatalf("avatarID = %q, want %q", p.AvatarID, ref.ID)
	}
	att, _ := e.store.GetAttachment(ref.ID)
	if att.State != model.AttachmentAvatar {
		t.Fatalf("avatar state = %s", att.State)
	}

	// Anyone authenticated can download an avatar; peers see avatarID.
	if code, body, _ := e.download(ref.ID, bTok, false); code != http.StatusOK || len(body) == 0 {
		t.Fatalf("avatar download by other user: %d", code)
	}
	var sum model.UserSummary
	e.call("GET", "/api/users/lookup?username=alice", bTok, nil, &sum)
	if sum.AvatarID != ref.ID || sum.Status != "shipping tier 4 🚀" {
		t.Fatalf("lookup summary = %+v", sum)
	}

	// Replacement reclaims the old avatar.
	_, ref2 := e.upload(aTok, "me2.png", "", testPNG(t, 64, 64))
	e.call("PATCH", "/api/settings/profile", aTok, map[string]any{"avatarAttachmentID": ref2.ID}, &p)
	if p.AvatarID != ref2.ID {
		t.Fatalf("replacement avatarID = %q", p.AvatarID)
	}
	if ok, _ := e.store.AttachmentExists(ref.ID); ok {
		t.Fatal("old avatar attachment not reclaimed")
	}
	if code, _, _ := e.download(ref.ID, aTok, false); code != http.StatusNotFound {
		t.Fatalf("old avatar still downloadable: %d", code)
	}

	// Removal.
	e.call("PATCH", "/api/settings/profile", aTok, map[string]any{"avatarAttachmentID": ""}, &p)
	if p.AvatarID != "" {
		t.Fatalf("avatar not removed: %+v", p)
	}
	if ok, _ := e.store.AttachmentExists(ref2.ID); ok {
		t.Fatal("removed avatar attachment not reclaimed")
	}

	_ = aUser
}

func TestAvatarValidation(t *testing.T) {
	e := newEnv(t)
	aTok, _ := e.register("alice")
	bTok, _ := e.register("bob")

	// Someone else's attachment.
	_, bobsRef := e.upload(bTok, "bobs.png", "", testPNG(t, 32, 32))
	if code := e.call("PATCH", "/api/settings/profile", aTok,
		map[string]any{"avatarAttachmentID": bobsRef.ID}, nil); code != http.StatusBadRequest {
		t.Fatalf("foreign avatar accepted: %d", code)
	}

	// Already attached elsewhere (committed to a message).
	var dm convResponse
	e.call("POST", "/api/conversations", aTok,
		map[string]any{"type": "dm", "participantIDs": []string{userID(t, e, "bob")}}, &dm)
	_, usedRef := e.upload(aTok, "used.png", "", testPNG(t, 32, 32))
	if _, err := e.store.AppendMessage(dm.ID, userID(t, e, "alice"), "cm-av", "", []string{usedRef.ID}); err != nil {
		t.Fatal(err)
	}
	if code := e.call("PATCH", "/api/settings/profile", aTok,
		map[string]any{"avatarAttachmentID": usedRef.ID}, nil); code != http.StatusBadRequest {
		t.Fatalf("committed attachment accepted as avatar: %d", code)
	}

	// Non-image bytes.
	_, txtRef := e.upload(aTok, "notes.txt", "text/plain", []byte("just some text, definitely not an image"))
	if code := e.call("PATCH", "/api/settings/profile", aTok,
		map[string]any{"avatarAttachmentID": txtRef.ID}, nil); code != http.StatusBadRequest {
		t.Fatalf("text file accepted as avatar: %d", code)
	}

	// Unknown attachment.
	if code := e.call("PATCH", "/api/settings/profile", aTok,
		map[string]any{"avatarAttachmentID": "a8f92c1f-7d3e-4e21-a44c-9f3d1a2b7c88"}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown avatar accepted: %d", code)
	}
}

// userID resolves a username through the store for test setup.
func userID(t *testing.T, e *testEnv, name string) string {
	t.Helper()
	u, err := e.store.GetUserByUsername(name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
