package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"jmessage/internal/model"
)

func pendingAttachment(t *testing.T, s *Store, id, owner string) *model.Attachment {
	t.Helper()
	a := &model.Attachment{
		ID: id, OwnerID: owner, Filename: "photo.png", MimeType: "image/png",
		Size: 1234, SHA256: "deadbeef", State: model.AttachmentPending,
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.CreateAttachment(a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAttachmentSendLifecycle(t *testing.T) {
	s, _, alice, bob, conv := tier1Fixture(t)
	pendingAttachment(t, s, "att-1", alice.ID)

	// Unknown attachment rejected.
	if _, err := s.AppendMessage(conv.ID, alice.ID, "c1", "x", []string{"att-nope"}); !errors.Is(err, ErrBadAttachment) {
		t.Fatalf("unknown attachment err = %v", err)
	}
	// Foreign attachment rejected.
	if _, err := s.AppendMessage(conv.ID, bob.ID, "c2", "x", []string{"att-1"}); !errors.Is(err, ErrNotAttachmentOwner) {
		t.Fatalf("foreign attachment err = %v", err)
	}
	// Nothing was persisted by the failures.
	if last, _ := s.LastSeq(conv.ID); last != 0 {
		t.Fatalf("failed sends persisted messages: last=%d", last)
	}

	// Valid send embeds refs and commits the attachment.
	msg, err := s.AppendMessage(conv.ID, alice.ID, "c3", "", []string{"att-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].ID != "att-1" || msg.Attachments[0].Filename != "photo.png" {
		t.Fatalf("refs = %+v", msg.Attachments)
	}
	a, _ := s.GetAttachment("att-1")
	if a.State != model.AttachmentCommitted || a.ConvID != conv.ID {
		t.Fatalf("after send: %+v", a)
	}
	// Committed preview falls back to the filename for empty bodies.
	c, _ := s.GetConversation(conv.ID)
	if c.LastPreview != "📎 photo.png" {
		t.Fatalf("preview = %q", c.LastPreview)
	}

	// Single-use: a second message cannot reference it.
	if _, err := s.AppendMessage(conv.ID, alice.ID, "c4", "again", []string{"att-1"}); !errors.Is(err, ErrBadAttachment) {
		t.Fatalf("reuse err = %v", err)
	}

	// Idempotent retry of the original send still returns the message.
	dup, err := s.AppendMessage(conv.ID, alice.ID, "c3", "", []string{"att-1"})
	if err != nil || dup.Seq != msg.Seq || len(dup.Attachments) != 1 {
		t.Fatalf("retry = %+v, %v", dup, err)
	}
}

// TestAttachmentRecommitAfterCrash: the message write survived but the
// attachment state flip (and hint) were lost. The allocator recovery
// scan must re-commit so the janitor can never prune a referenced
// attachment.
func TestAttachmentRecommitAfterCrash(t *testing.T) {
	s, dir, alice, _, conv := tier1Fixture(t)
	pendingAttachment(t, s, "att-crash", alice.ID)

	// Crash simulation: message doc referencing the attachment lands,
	// nothing else does.
	orphan := model.Message{
		ConvID: conv.ID, Seq: 1, SenderID: alice.ID, Body: "pic",
		TS: time.Now().UnixMilli(), ClientMsgID: "cm-crash",
		Attachments: []model.AttachmentRef{{ID: "att-crash", Filename: "photo.png", MimeType: "image/png", Size: 1234}},
	}
	raw, _ := json.Marshal(orphan)
	if err := s.db.Put(msgKey(conv.ID, 1), raw); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// Any allocator load triggers recovery (LastSeq loads lazily).
	if last, _ := s2.LastSeq(conv.ID); last != 1 {
		t.Fatalf("LastSeq = %d", last)
	}
	a, err := s2.GetAttachment("att-crash")
	if err != nil || a.State != model.AttachmentCommitted || a.ConvID != conv.ID {
		t.Fatalf("after recovery: %+v, %v", a, err)
	}
	// And it is invisible to the stale-pending sweep.
	stale, _ := s2.ListStalePendingAttachments(0)
	for _, sa := range stale {
		if sa.ID == "att-crash" {
			t.Fatal("recovered attachment still visible to the janitor")
		}
	}
	// The retry dedups as usual.
	dup, err := s2.AppendMessage(conv.ID, alice.ID, "cm-crash", "pic", []string{"att-crash"})
	if err != nil || dup.Seq != 1 {
		t.Fatalf("retry = %+v, %v", dup, err)
	}
}

func TestListStalePendingAttachments(t *testing.T) {
	s, _, alice, _, conv := tier1Fixture(t)

	old := pendingAttachment(t, s, "att-old", alice.ID)
	old.CreatedAt = time.Now().Add(-48 * time.Hour).UnixMilli()
	if err := s.CreateAttachment(old); err != nil {
		t.Fatal(err)
	}
	pendingAttachment(t, s, "att-fresh", alice.ID)
	committed := pendingAttachment(t, s, "att-done", alice.ID)
	if _, err := s.AppendMessage(conv.ID, alice.ID, "", "x", []string{committed.ID}); err != nil {
		t.Fatal(err)
	}

	stale, err := s.ListStalePendingAttachments(24 * time.Hour)
	if err != nil || len(stale) != 1 || stale[0].ID != "att-old" {
		t.Fatalf("stale = %+v, %v", stale, err)
	}

	if err := s.DeleteAttachment("att-old"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.AttachmentExists("att-old"); ok {
		t.Fatal("deleted attachment still exists")
	}
	if ok, _ := s.AttachmentExists("att-done"); !ok {
		t.Fatal("committed attachment vanished")
	}
}
