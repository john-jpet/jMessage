package store

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"jmessage/internal/model"
)

// tier1Fixture opens a store with two users and a DM.
func tier1Fixture(t *testing.T) (s *Store, dir string, alice, bob *model.User, conv *model.Conversation) {
	t.Helper()
	dir = t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	alice, err = s.CreateUser("alice", "Alice", "h")
	if err != nil {
		t.Fatal(err)
	}
	bob, err = s.CreateUser("bob", "Bob", "h")
	if err != nil {
		t.Fatal(err)
	}
	conv, err = s.CreateConversation(model.ConvDM, "", alice.ID, []string{bob.ID})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, alice, bob, conv
}

func TestIdempotentAppend(t *testing.T) {
	s, _, alice, _, conv := tier1Fixture(t)

	m1, err := s.AppendMessage(conv.ID, alice.ID, "cmid-1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	// Retry with the same clientMsgID: same message back, nothing appended.
	m2, err := s.AppendMessage(conv.ID, alice.ID, "cmid-1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if m2.Seq != m1.Seq || m2.Body != m1.Body || m2.TS != m1.TS {
		t.Fatalf("retry returned different message: %+v vs %+v", m2, m1)
	}
	if last, _ := s.LastSeq(conv.ID); last != 1 {
		t.Fatalf("LastSeq = %d after retried send, want 1", last)
	}
	// A different clientMsgID appends normally.
	m3, err := s.AppendMessage(conv.ID, alice.ID, "cmid-2", "world")
	if err != nil || m3.Seq != 2 {
		t.Fatalf("m3 = %+v, %v", m3, err)
	}
	// Retries are per-sender: bob using the same ID is a different message.
	m4, err := s.AppendMessage(conv.ID, "0000000002", "cmid-1", "bob speaking")
	if err != nil || m4.Seq != 3 {
		t.Fatalf("m4 = %+v, %v", m4, err)
	}
}

// TestIdempotencyRebuildAfterCrash simulates the §4 crash window: the
// message write survived but the clientmsg: index (and hint) were lost.
// On reopen the allocator's recovery scan must rebuild the index so a
// retry dedups instead of duplicating.
func TestIdempotencyRebuildAfterCrash(t *testing.T) {
	s, dir, alice, _, conv := tier1Fixture(t)

	if _, err := s.AppendMessage(conv.ID, alice.ID, "cmid-1", "settled"); err != nil {
		t.Fatal(err)
	}

	// Crash simulation: write the message doc directly (embedding its
	// clientMsgID) without the clientmsg: index or hint updates.
	orphan := model.Message{
		ConvID: conv.ID, Seq: 2, SenderID: alice.ID,
		Body: "orphaned by crash", TS: time.Now().UnixMilli(),
		ClientMsgID: "cmid-orphan",
	}
	raw, _ := json.Marshal(orphan)
	if err := s.db.Put(msgKey(conv.ID, 2), raw); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// The client retries the send that never got acked.
	m, err := s2.AppendMessage(conv.ID, alice.ID, "cmid-orphan", "orphaned by crash")
	if err != nil {
		t.Fatal(err)
	}
	if m.Seq != 2 {
		t.Fatalf("retry after crash appended a duplicate: seq %d, want 2", m.Seq)
	}
	if last, _ := s2.LastSeq(conv.ID); last != 2 {
		t.Fatalf("LastSeq = %d, want 2", last)
	}
	msgs, _, _ := s2.ListMessagesAfter(conv.ID, 0, 0)
	if len(msgs) != 2 {
		t.Fatalf("message count = %d, want 2 (no duplicate)", len(msgs))
	}
}

func TestReadStateWatermarks(t *testing.T) {
	s, _, alice, _, conv := tier1Fixture(t)

	// SetRead raises both watermarks.
	rs, changed, err := s.SetRead(alice.ID, conv.ID, 10)
	if err != nil || !changed || rs.ReadSeq != 10 || rs.DeliveredSeq != 10 {
		t.Fatalf("SetRead: %+v changed=%v err=%v", rs, changed, err)
	}
	// Regression is ignored.
	rs, changed, _ = s.SetRead(alice.ID, conv.ID, 5)
	if changed || rs.ReadSeq != 10 {
		t.Fatalf("regression applied: %+v changed=%v", rs, changed)
	}
	// Delivered can run ahead of read.
	rs, changed, _ = s.SetDelivered(alice.ID, conv.ID, 15)
	if !changed || rs.DeliveredSeq != 15 || rs.ReadSeq != 10 {
		t.Fatalf("SetDelivered: %+v changed=%v", rs, changed)
	}
	// Persisted.
	got, err := s.GetReadState(alice.ID, conv.ID)
	if err != nil || got.DeliveredSeq != 15 || got.ReadSeq != 10 {
		t.Fatalf("GetReadState = %+v, %v", got, err)
	}
	// Unknown pair is zero-valued, not an error.
	zero, err := s.GetReadState("0000009999", conv.ID)
	if err != nil || zero != (model.ReadState{}) {
		t.Fatalf("zero state = %+v, %v", zero, err)
	}
}

func TestReadStateConcurrent(t *testing.T) {
	s, _, alice, _, conv := tier1Fixture(t)

	var wg sync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func(seq uint64) {
			defer wg.Done()
			if _, _, err := s.SetRead(alice.ID, conv.ID, seq); err != nil {
				t.Error(err)
			}
		}(uint64(i))
	}
	wg.Wait()
	rs, _ := s.GetReadState(alice.ID, conv.ID)
	if rs.ReadSeq != 50 || rs.DeliveredSeq != 50 {
		t.Fatalf("concurrent watermark = %+v, want 50/50", rs)
	}
}

func TestListMessagesAfter(t *testing.T) {
	s, _, alice, _, conv := tier1Fixture(t)
	for i := 1; i <= 30; i++ {
		if _, err := s.AppendMessage(conv.ID, alice.ID, "", fmt.Sprintf("m%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	// after=0 returns everything.
	msgs, hasMore, err := s.ListMessagesAfter(conv.ID, 0, 0)
	if err != nil || len(msgs) != 30 || hasMore {
		t.Fatalf("after=0: len=%d hasMore=%v err=%v", len(msgs), hasMore, err)
	}
	if msgs[0].Seq != 1 || msgs[29].Seq != 30 {
		t.Fatalf("bounds: %d..%d", msgs[0].Seq, msgs[29].Seq)
	}
	// Mid-stream boundary.
	msgs, hasMore, _ = s.ListMessagesAfter(conv.ID, 25, 0)
	if len(msgs) != 5 || hasMore || msgs[0].Seq != 26 {
		t.Fatalf("after=25: len=%d hasMore=%v first=%d", len(msgs), hasMore, msgs[0].Seq)
	}
	// At the tip: empty, no more.
	msgs, hasMore, _ = s.ListMessagesAfter(conv.ID, 30, 0)
	if len(msgs) != 0 || hasMore {
		t.Fatalf("at tip: len=%d hasMore=%v", len(msgs), hasMore)
	}
	// Limit cap sets hasMore.
	msgs, hasMore, _ = s.ListMessagesAfter(conv.ID, 0, 10)
	if len(msgs) != 10 || !hasMore || msgs[9].Seq != 10 {
		t.Fatalf("limited: len=%d hasMore=%v last=%d", len(msgs), hasMore, msgs[9].Seq)
	}
}

func TestDevices(t *testing.T) {
	s, _, alice, _, _ := tier1Fixture(t)

	if err := s.UpsertDevice(alice.ID, "dev-chrome", "Chrome on Windows"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDevice(alice.ID, "dev-phone", "Mobile Safari"); err != nil {
		t.Fatal(err)
	}
	devs, err := s.ListDevices(alice.ID)
	if err != nil || len(devs) != 2 {
		t.Fatalf("devices = %+v, %v", devs, err)
	}
	before := devs[0].LastSeen
	time.Sleep(5 * time.Millisecond)
	if err := s.TouchDevice(alice.ID, devs[0].ID); err != nil {
		t.Fatal(err)
	}
	devs2, _ := s.ListDevices(alice.ID)
	if devs2[0].LastSeen <= before {
		t.Fatalf("TouchDevice did not advance lastSeen: %d <= %d", devs2[0].LastSeen, before)
	}
}

func TestPruneClientMsgs(t *testing.T) {
	s, _, alice, _, conv := tier1Fixture(t)

	if _, err := s.AppendMessage(conv.ID, alice.ID, "cmid-fresh", "recent"); err != nil {
		t.Fatal(err)
	}
	// Plant an ancient entry directly.
	old, _ := json.Marshal(clientMsgRef{ConvID: conv.ID, Seq: 999, TS: time.Now().Add(-30 * 24 * time.Hour).UnixMilli()})
	if err := s.db.Put(clientMsgKey(alice.ID, "cmid-ancient"), old); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneClientMsgs(7 * 24 * time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("pruned %d, err %v; want 1", n, err)
	}
	// Fresh entry still dedups.
	m, err := s.AppendMessage(conv.ID, alice.ID, "cmid-fresh", "recent")
	if err != nil || m.Seq != 1 {
		t.Fatalf("fresh entry lost: %+v, %v", m, err)
	}
}
