package store

import (
	"fmt"
	"sync"
	"testing"
)

func TestReactionsBasics(t *testing.T) {
	s, _, alice, bob, conv := tier1Fixture(t)
	if _, err := s.AppendMessage(conv.ID, alice.ID, "", "react to me", nil); err != nil {
		t.Fatal(err)
	}

	// Add is idempotent: same user + emoji twice → count stays 1.
	if n, err := s.AddReaction(conv.ID, 1, "👍", alice.ID); err != nil || n != 1 {
		t.Fatalf("add: n=%d err=%v", n, err)
	}
	if n, _ := s.AddReaction(conv.ID, 1, "👍", alice.ID); n != 1 {
		t.Fatalf("duplicate add counted: %d", n)
	}
	if n, _ := s.AddReaction(conv.ID, 1, "👍", bob.ID); n != 2 {
		t.Fatalf("second user: %d", n)
	}
	if n, _ := s.AddReaction(conv.ID, 1, "❤️", bob.ID); n != 1 {
		t.Fatalf("second emoji: %d", n)
	}

	// Remove is idempotent too.
	if n, _ := s.RemoveReaction(conv.ID, 1, "👍", alice.ID); n != 1 {
		t.Fatalf("remove: %d", n)
	}
	if n, _ := s.RemoveReaction(conv.ID, 1, "👍", alice.ID); n != 1 {
		t.Fatalf("duplicate remove changed count: %d", n)
	}

	// Unknown message rejected.
	if _, err := s.AddReaction(conv.ID, 999, "👍", alice.ID); err == nil {
		t.Fatal("reaction on missing message accepted")
	}

	// Aggregation with viewer-relative reacted.
	aggs, err := s.ReactionsForRange(conv.ID, 1, 1, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := aggs[1]
	if len(m) != 2 {
		t.Fatalf("aggs = %+v", m)
	}
	for _, a := range m {
		if a.Count != 1 || !a.Reacted { // bob reacted with both remaining
			t.Fatalf("agg %+v", a)
		}
	}
	// Alice removed hers: not reacted from her view.
	aggs, _ = s.ReactionsForRange(conv.ID, 1, 1, alice.ID)
	for _, a := range aggs[1] {
		if a.Reacted {
			t.Fatalf("alice still marked reacted: %+v", a)
		}
	}
}

func TestReactionsRangeAndAttach(t *testing.T) {
	s, _, alice, bob, conv := tier1Fixture(t)
	for i := 0; i < 5; i++ {
		if _, err := s.AppendMessage(conv.ID, alice.ID, "", fmt.Sprintf("m%d", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	s.AddReaction(conv.ID, 1, "👍", bob.ID)
	s.AddReaction(conv.ID, 3, "😂", alice.ID)
	s.AddReaction(conv.ID, 3, "😂", bob.ID)
	s.AddReaction(conv.ID, 5, "👎", bob.ID)

	msgs, _, _ := s.ListMessagesAfter(conv.ID, 1, 0) // seqs 2..5
	if err := s.AttachReactions(conv.ID, msgs, alice.ID); err != nil {
		t.Fatal(err)
	}
	bySeq := map[uint64][]string{}
	for _, m := range msgs {
		for _, a := range m.Reactions {
			bySeq[m.Seq] = append(bySeq[m.Seq], fmt.Sprintf("%s=%d(r=%v)", a.Emoji, a.Count, a.Reacted))
		}
	}
	if len(bySeq[2]) != 0 || len(bySeq[3]) != 1 || len(bySeq[5]) != 1 {
		t.Fatalf("range aggs = %v", bySeq)
	}
	if bySeq[3][0] != "😂=2(r=true)" {
		t.Fatalf("seq3 = %v", bySeq[3])
	}
	// Range scan must NOT include seq 1 (outside [2,5]).
	if _, ok := bySeq[1]; ok {
		t.Fatal("out-of-range seq leaked into aggregation")
	}
}

func TestReactionsConcurrent(t *testing.T) {
	s, _, alice, _, conv := tier1Fixture(t)
	if _, err := s.AppendMessage(conv.ID, alice.ID, "", "storm", nil); err != nil {
		t.Fatal(err)
	}
	// 30 users hammer add/remove concurrently; final state must equal
	// the deterministic last operation per user (add).
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			uid := fmt.Sprintf("%010d", 100+i)
			s.AddReaction(conv.ID, 1, "👍", uid)
			s.RemoveReaction(conv.ID, 1, "👍", uid)
			s.AddReaction(conv.ID, 1, "👍", uid)
		}(i)
	}
	wg.Wait()
	aggs, _ := s.ReactionsForRange(conv.ID, 1, 1, alice.ID)
	if len(aggs[1]) != 1 || aggs[1][0].Count != 30 {
		t.Fatalf("final aggs = %+v", aggs[1])
	}
}
