package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"jmessage/internal/model"
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreateUserAndUniqueness(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()

	alice, err := s.CreateUser("Alice", "Alice A", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if alice.ID != pad10(1) {
		t.Fatalf("first user ID = %q", alice.ID)
	}

	// Case-insensitive uniqueness.
	if _, err := s.CreateUser("alice", "Other", "hash2"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username: %v", err)
	}

	got, err := s.GetUserByUsername("ALICE")
	if err != nil || got.ID != alice.ID {
		t.Fatalf("GetUserByUsername = %+v, %v", got, err)
	}
	if _, err := s.GetUserByUsername("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent username: %v", err)
	}
}

func TestCounterRecoversFromStaleHint(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	u1, _ := s.CreateUser("a", "A", "h")
	u2, _ := s.CreateUser("b", "B", "h")
	// Simulate crash-before-hint: write a user doc directly with a higher
	// ID, leaving the counter hint stale.
	orphanID := pad10(7)
	if err := s.putJSON(userKey(orphanID), &model.User{ID: orphanID, Username: "ghost"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2 := open(t, dir)
	defer s2.Close()
	u3, err := s2.CreateUser("c", "C", "h")
	if err != nil {
		t.Fatal(err)
	}
	if u3.ID != pad10(8) {
		t.Fatalf("post-recovery ID = %q, want %q (u1=%s u2=%s)", u3.ID, pad10(8), u1.ID, u2.ID)
	}
}

func TestDMCreationAndDedup(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	a, _ := s.CreateUser("a", "A", "h")
	b, _ := s.CreateUser("b", "B", "h")

	dm1, err := s.CreateConversation(model.ConvDM, "", a.ID, []string{b.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Same pair, either direction, dedups to the same conversation.
	dm2, err := s.CreateConversation(model.ConvDM, "", b.ID, []string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if dm1.ID != dm2.ID {
		t.Fatalf("dm not deduped: %s vs %s", dm1.ID, dm2.ID)
	}

	for _, uid := range []string{a.ID, b.ID} {
		ok, err := s.IsMember(dm1.ID, uid)
		if err != nil || !ok {
			t.Fatalf("IsMember(%s) = %v, %v", uid, ok, err)
		}
	}
	if ok, _ := s.IsMember(dm1.ID, pad10(99)); ok {
		t.Fatal("non-member reported as member")
	}

	if _, err := s.CreateConversation(model.ConvDM, "", a.ID, []string{a.ID}); err == nil {
		t.Fatal("self-DM accepted")
	}
}

func TestGroupAndListConversations(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	a, _ := s.CreateUser("a", "A", "h")
	b, _ := s.CreateUser("b", "B", "h")
	c, _ := s.CreateUser("c", "C", "h")

	g, err := s.CreateConversation(model.ConvGroup, "team", a.ID, []string{b.ID})
	if err != nil {
		t.Fatal(err)
	}
	dm, _ := s.CreateConversation(model.ConvDM, "", a.ID, []string{c.ID})

	if err := s.AddMember(g.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	members, err := s.ListMembers(g.ID)
	if err != nil || len(members) != 3 {
		t.Fatalf("ListMembers = %v, %v", members, err)
	}
	if err := s.AddMember(dm.ID, b.ID); err == nil {
		t.Fatal("AddMember on a dm accepted")
	}

	// Activity ordering: message in the group makes it newest.
	if _, err := s.AppendMessage(g.ID, a.ID, "", "hello team"); err != nil {
		t.Fatal(err)
	}
	convs, err := s.ListUserConversations(a.ID)
	if err != nil || len(convs) != 2 {
		t.Fatalf("ListUserConversations = %d convs, %v", len(convs), err)
	}
	if convs[0].ID != g.ID {
		t.Fatalf("newest-activity conv = %s, want group %s", convs[0].ID, g.ID)
	}
	if convs[0].LastPreview != "hello team" {
		t.Fatalf("preview = %q", convs[0].LastPreview)
	}
	// c is only in the group + dm with a.
	convsC, _ := s.ListUserConversations(c.ID)
	if len(convsC) != 2 {
		t.Fatalf("c sees %d convs", len(convsC))
	}
}

func TestDenseSequencesUnderConcurrency(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	a, _ := s.CreateUser("a", "A", "h")
	b, _ := s.CreateUser("b", "B", "h")
	conv, _ := s.CreateConversation(model.ConvDM, "", a.ID, []string{b.ID})

	const senders, per = 20, 25
	var wg sync.WaitGroup
	seqs := make(chan uint64, senders*per)
	for w := 0; w < senders; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				m, err := s.AppendMessage(conv.ID, a.ID, "", fmt.Sprintf("w%d-%d", w, i))
				if err != nil {
					panic(err)
				}
				seqs <- m.Seq
			}
		}(w)
	}
	wg.Wait()
	close(seqs)

	seen := make(map[uint64]bool)
	for seq := range seqs {
		if seen[seq] {
			t.Fatalf("duplicate seq %d", seq)
		}
		seen[seq] = true
	}
	for i := uint64(1); i <= senders*per; i++ {
		if !seen[i] {
			t.Fatalf("gap: seq %d missing", i)
		}
	}
	last, _ := s.LastSeq(conv.ID)
	if last != senders*per {
		t.Fatalf("LastSeq = %d, want %d", last, senders*per)
	}
}

func TestSeqRecoveryAfterCrashBeforeHint(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	a, _ := s.CreateUser("a", "A", "h")
	b, _ := s.CreateUser("b", "B", "h")
	conv, _ := s.CreateConversation(model.ConvDM, "", a.ID, []string{b.ID})
	for i := 0; i < 5; i++ {
		if _, err := s.AppendMessage(conv.ID, a.ID, "", fmt.Sprintf("m%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate crash-after-message-before-hint: write message keys 6 and 7
	// directly, leaving the convseq hint at 5.
	for seq := uint64(6); seq <= 7; seq++ {
		m := &model.Message{ConvID: conv.ID, Seq: seq, SenderID: a.ID, Body: "direct", TS: 1}
		if err := s.putJSON(msgKey(conv.ID, seq), m); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s2 := open(t, dir)
	defer s2.Close()
	m, err := s2.AppendMessage(conv.ID, a.ID, "", "after recovery")
	if err != nil {
		t.Fatal(err)
	}
	if m.Seq != 8 {
		t.Fatalf("post-recovery seq = %d, want 8 (hint must not shadow scan)", m.Seq)
	}
}

func TestPaginationBoundaries(t *testing.T) {
	s := open(t, t.TempDir())
	defer s.Close()
	a, _ := s.CreateUser("a", "A", "h")
	b, _ := s.CreateUser("b", "B", "h")
	conv, _ := s.CreateConversation(model.ConvDM, "", a.ID, []string{b.ID})
	const total = 105
	for i := 1; i <= total; i++ {
		if _, err := s.AppendMessage(conv.ID, a.ID, "", fmt.Sprintf("msg-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	// Newest page: before=0 → last 50, ascending.
	msgs, hasMore, err := s.ListMessagesBefore(conv.ID, 0, 50)
	if err != nil || len(msgs) != 50 || !hasMore {
		t.Fatalf("newest page: %d msgs, hasMore=%v, %v", len(msgs), hasMore, err)
	}
	if msgs[0].Seq != 56 || msgs[49].Seq != 105 {
		t.Fatalf("newest page range [%d,%d]", msgs[0].Seq, msgs[49].Seq)
	}

	// Walk backwards to the beginning.
	before := msgs[0].Seq
	var walked int
	for {
		page, more, err := s.ListMessagesBefore(conv.ID, before, 50)
		if err != nil {
			t.Fatal(err)
		}
		walked += len(page)
		if !more {
			if len(page) > 0 && page[0].Seq != 1 {
				t.Fatalf("final page starts at %d", page[0].Seq)
			}
			break
		}
		before = page[0].Seq
	}
	if walked != total-50 {
		t.Fatalf("walked %d older messages, want %d", walked, total-50)
	}

	// Exact-boundary page: before=51, limit=50 → seqs 1..50, no more.
	page, more, _ := s.ListMessagesBefore(conv.ID, 51, 50)
	if len(page) != 50 || more || page[0].Seq != 1 || page[49].Seq != 50 {
		t.Fatalf("boundary page: len=%d more=%v range[%d,%d]", len(page), more, page[0].Seq, page[len(page)-1].Seq)
	}

	// before=1 → empty.
	page, more, _ = s.ListMessagesBefore(conv.ID, 1, 50)
	if len(page) != 0 || more {
		t.Fatalf("before=1 page: len=%d more=%v", len(page), more)
	}

	// Empty conversation.
	conv2, _ := s.CreateConversation(model.ConvGroup, "empty", a.ID, []string{b.ID})
	page, more, err = s.ListMessagesBefore(conv2.ID, 0, 50)
	if err != nil || len(page) != 0 || more {
		t.Fatalf("empty conv: len=%d more=%v %v", len(page), more, err)
	}
}

func TestReopenPreservesEverything(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	a, _ := s.CreateUser("a", "A", "h")
	b, _ := s.CreateUser("b", "B", "h")
	conv, _ := s.CreateConversation(model.ConvDM, "", a.ID, []string{b.ID})
	for i := 0; i < 20; i++ {
		s.AppendMessage(conv.ID, a.ID, "", fmt.Sprintf("m%d", i))
	}
	s.Close()

	s2 := open(t, dir)
	defer s2.Close()
	if _, err := s2.GetUserByUsername("a"); err != nil {
		t.Fatal(err)
	}
	msgs, _, err := s2.ListMessagesBefore(conv.ID, 0, 100)
	if err != nil || len(msgs) != 20 {
		t.Fatalf("after reopen: %d msgs, %v", len(msgs), err)
	}
	convs, _ := s2.ListUserConversations(b.ID)
	if len(convs) != 1 || convs[0].ID != conv.ID {
		t.Fatalf("after reopen: convs = %+v", convs)
	}
}
