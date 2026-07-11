package store

import (
	"sort"
	"strings"

	"jmessage/internal/model"
)

// Reactions are one key per (message, emoji, user) with an empty value.
// Duplicate adds overwrite the same key and counts come from prefix
// scans, so the whole feature is idempotent and race-free without any
// counters or mutexes.

// AddReaction records uid's emoji on a message and returns the emoji's
// fresh count. The message must exist; callers enforce membership and
// the emoji allow-list.
func (s *Store) AddReaction(cid string, seq uint64, emoji, uid string) (int, error) {
	if _, err := s.db.Get(msgKey(cid, seq)); err != nil {
		return 0, ErrNotFound
	}
	if err := s.db.Put(reactionKey(cid, seq, emoji, uid), []byte("{}")); err != nil {
		return 0, err
	}
	return s.countReaction(cid, seq, emoji)
}

// RemoveReaction deletes uid's emoji from a message (idempotent) and
// returns the emoji's fresh count.
func (s *Store) RemoveReaction(cid string, seq uint64, emoji, uid string) (int, error) {
	if err := s.db.Delete(reactionKey(cid, seq, emoji, uid)); err != nil {
		return 0, err
	}
	return s.countReaction(cid, seq, emoji)
}

func (s *Store) countReaction(cid string, seq uint64, emoji string) (int, error) {
	prefix := reactionEmojiPrefix(cid, seq, emoji)
	it, err := s.db.Scan(prefix, prefixEnd(prefix))
	if err != nil {
		return 0, err
	}
	defer it.Close()
	n := 0
	for ; it.Valid(); it.Next() {
		n++
	}
	return n, it.Err()
}

// ReactionsForRange aggregates reactions for every message with
// from <= seq <= to in one range scan (reaction keys sort by seq14).
// Reacted is computed for the viewer.
func (s *Store) ReactionsForRange(cid string, from, to uint64, viewer string) (map[uint64][]model.ReactionAgg, error) {
	if to < from {
		return nil, nil
	}
	it, err := s.db.Scan(reactionSeqPrefix(cid, from), reactionSeqPrefix(cid, to+1))
	if err != nil {
		return nil, err
	}
	defer it.Close()

	// seq -> emoji -> agg (insertion order preserved separately).
	type bucket struct {
		order []string
		aggs  map[string]*model.ReactionAgg
	}
	buckets := make(map[uint64]*bucket)

	base := len("reaction:" + cid + ":")
	for ; it.Valid(); it.Next() {
		key := string(it.Key())
		rest := key[base:] // <seq14>:<emoji>:<uid>
		if len(rest) < 14+1+1+1+10 {
			continue
		}
		seq, err := parseSeq(rest[:14])
		if err != nil {
			continue
		}
		// uid is the fixed-width tail; emoji sits between the delimiters.
		uid := rest[len(rest)-10:]
		emoji := rest[15 : len(rest)-11]
		if strings.ContainsRune(emoji, ':') {
			continue // defensive: never produced by reactionKey
		}

		b := buckets[seq]
		if b == nil {
			b = &bucket{aggs: make(map[string]*model.ReactionAgg)}
			buckets[seq] = b
		}
		agg := b.aggs[emoji]
		if agg == nil {
			agg = &model.ReactionAgg{Emoji: emoji}
			b.aggs[emoji] = agg
			b.order = append(b.order, emoji)
		}
		agg.Count++
		if uid == viewer {
			agg.Reacted = true
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}

	out := make(map[uint64][]model.ReactionAgg, len(buckets))
	for seq, b := range buckets {
		sort.Strings(b.order) // deterministic chip order
		aggs := make([]model.ReactionAgg, 0, len(b.order))
		for _, e := range b.order {
			aggs = append(aggs, *b.aggs[e])
		}
		out[seq] = aggs
	}
	return out, nil
}

// AttachReactions decorates a message slice in place for the viewer.
func (s *Store) AttachReactions(cid string, msgs []model.Message, viewer string) error {
	if len(msgs) == 0 {
		return nil
	}
	aggs, err := s.ReactionsForRange(cid, msgs[0].Seq, msgs[len(msgs)-1].Seq, viewer)
	if err != nil {
		return err
	}
	if len(aggs) == 0 {
		return nil
	}
	for i := range msgs {
		msgs[i].Reactions = aggs[msgs[i].Seq]
	}
	return nil
}

func parseSeq(s string) (uint64, error) {
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, ErrNotFound
		}
		n = n*10 + uint64(c-'0')
	}
	return n, nil
}
