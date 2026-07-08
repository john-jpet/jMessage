package store

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	petdb "petdb"

	"jmessage/internal/model"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// seqAllocator hands out dense (gap-free) per-conversation message
// sequence numbers, which is what makes backwards pagination pure
// arithmetic on a forward-only engine.
//
// Durability protocol per send (all under the conversation's mutex,
// which doubles as the per-conversation send serializer):
//
//	seq := last+1
//	Put message:<cid>:<seq>     ← fsync-durable; the message now exists
//	last = seq                  ← in-memory only after success: no gaps
//	Put convseq:<cid> hint      ← best-effort recovery accelerator
//
// After a crash the hint may trail reality by the writes in flight;
// loading scans message keys forward from hint+1 to find the true last.
type seqAllocator struct {
	mu    sync.Mutex // guards convs map
	convs map[string]*convSeq
	db    *petdb.DB
}

type convSeq struct {
	mu     sync.Mutex
	last   uint64
	loaded bool
}

func newSeqAllocator(db *petdb.DB) *seqAllocator {
	return &seqAllocator{convs: make(map[string]*convSeq), db: db}
}

func (a *seqAllocator) get(cid string) *convSeq {
	a.mu.Lock()
	defer a.mu.Unlock()
	cs, ok := a.convs[cid]
	if !ok {
		cs = &convSeq{}
		a.convs[cid] = cs
	}
	return cs
}

// clientMsgRef is the value of a clientmsg:<uid>:<clientMsgID> key: it
// points a client-generated message ID at the message it produced, so a
// retried send returns the original instead of appending a duplicate.
type clientMsgRef struct {
	ConvID string `json:"convID"`
	Seq    uint64 `json:"seq"`
	TS     int64  `json:"ts"` // creation time, used by the janitor
}

// loadLocked recovers the authoritative last seq. Caller holds cs.mu.
//
// The scan covers exactly the messages written after the last persisted
// hint — and since a message's clientmsg: index entry is written before
// the hint, any index entry lost to a crash belongs to a message inside
// this window. Rebuilding those entries here closes the §4 duplicate
// window: a retry after a crash always finds the index entry.
func (a *seqAllocator) loadLocked(cid string, cs *convSeq) error {
	var hint uint64
	if v, err := a.db.Get(convSeqKey(cid)); err == nil {
		hint, _ = strconv.ParseUint(string(v), 10, 64)
	} else if !errors.Is(err, petdb.ErrNotFound) {
		return err
	}

	last := hint
	var rebuild []model.Message
	it, err := a.db.Scan(msgKey(cid, hint+1), prefixEnd(msgPrefix(cid)))
	if err != nil {
		return err
	}
	for ; it.Valid(); it.Next() {
		if seq, err := seqFromMsgKey(it.Key()); err == nil && seq > last {
			last = seq
		}
		var m model.Message
		if err := jsonUnmarshal(it.Value(), &m); err == nil && m.ClientMsgID != "" {
			rebuild = append(rebuild, m)
		}
	}
	scanErr := it.Err()
	it.Close() // close before writing below
	if scanErr != nil {
		return scanErr
	}

	for _, m := range rebuild {
		ref, _ := json.Marshal(clientMsgRef{ConvID: m.ConvID, Seq: m.Seq, TS: m.TS})
		if err := a.db.Put(clientMsgKey(m.SenderID, m.ClientMsgID), ref); err != nil {
			return err
		}
	}

	cs.last = last
	cs.loaded = true
	return nil
}

// LastSeq returns the newest sequence number in a conversation (0 if empty).
func (s *Store) LastSeq(cid string) (uint64, error) {
	cs := s.seqs.get(cid)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !cs.loaded {
		if err := s.seqs.loadLocked(cid, cs); err != nil {
			return 0, err
		}
	}
	return cs.last, nil
}

// AppendMessage durably persists a message and returns it with its
// assigned sequence number. It returns only after the record is
// fsync-durable in PetDB's WAL — the caller may ack the sender.
//
// A non-empty clientMsgID makes the call idempotent: a retry with the
// same (sender, clientMsgID) returns the originally stored message
// without appending. Write order: message (embeds clientMsgID) →
// clientmsg: index → hint. The dedup check runs after loadLocked, which
// rebuilds any index entry a crash may have dropped, so retries across
// server restarts dedup correctly too.
func (s *Store) AppendMessage(cid, senderID, clientMsgID, body string) (*model.Message, error) {
	cs := s.seqs.get(cid)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !cs.loaded {
		if err := s.seqs.loadLocked(cid, cs); err != nil {
			return nil, err
		}
	}

	if clientMsgID != "" {
		var ref clientMsgRef
		err := s.getJSON(clientMsgKey(senderID, clientMsgID), &ref)
		if err == nil {
			var m model.Message
			if err := s.getJSON(msgKey(ref.ConvID, ref.Seq), &m); err != nil {
				return nil, err
			}
			return &m, nil // duplicate send: return the original
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	msg := &model.Message{
		ConvID:      cid,
		Seq:         cs.last + 1,
		SenderID:    senderID,
		Body:        body,
		TS:          time.Now().UnixMilli(),
		ClientMsgID: clientMsgID,
	}
	if err := s.putJSON(msgKey(cid, msg.Seq), msg); err != nil {
		return nil, err // last not advanced: the seq will be reused
	}
	cs.last = msg.Seq

	if clientMsgID != "" {
		ref := clientMsgRef{ConvID: cid, Seq: msg.Seq, TS: msg.TS}
		if err := s.putJSON(clientMsgKey(senderID, clientMsgID), &ref); err != nil {
			// Message is durable; a lost index entry is rebuilt on the
			// next allocator load (hint below is not yet written).
			s.logger.Warn("clientmsg index write failed", "conv", cid, "err", err)
			return msg, nil
		}
	}

	// Best-effort metadata; failures don't invalidate the message.
	if err := s.db.Put(convSeqKey(cid), []byte(pad14(msg.Seq))); err != nil {
		s.logger.Warn("convseq hint write failed", "conv", cid, "err", err)
	}
	if err := s.touchConversation(cid, msg); err != nil {
		s.logger.Warn("conversation activity update failed", "conv", cid, "err", err)
	}
	return msg, nil
}

// touchConversation refreshes the last-activity fields used to order
// the conversation list. Serialized by the conversation's seq mutex.
func (s *Store) touchConversation(cid string, msg *model.Message) error {
	var c model.Conversation
	if err := s.getJSON(convKey(cid), &c); err != nil {
		return err
	}
	c.LastActivityTS = msg.TS
	c.LastPreview = truncate(msg.Body, 80)
	c.LastSenderID = msg.SenderID
	return s.putJSON(convKey(cid), &c)
}

// ListMessagesBefore returns up to limit messages with seq < before,
// ascending, plus whether older messages remain. before == 0 means
// "from the newest". Dense seqs make the page bounds exact arithmetic.
func (s *Store) ListMessagesBefore(cid string, before uint64, limit int) (msgs []model.Message, hasMore bool, err error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if before == 0 {
		last, err := s.LastSeq(cid)
		if err != nil {
			return nil, false, err
		}
		before = last + 1
	}
	if before <= 1 {
		return nil, false, nil
	}
	start := uint64(1)
	if before > uint64(limit) {
		start = before - uint64(limit)
	}

	it, err := s.db.Scan(msgKey(cid, start), msgKey(cid, before))
	if err != nil {
		return nil, false, err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		var m model.Message
		if err := jsonUnmarshal(it.Value(), &m); err != nil {
			return nil, false, err
		}
		msgs = append(msgs, m)
	}
	if err := it.Err(); err != nil {
		return nil, false, err
	}
	return msgs, start > 1, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
