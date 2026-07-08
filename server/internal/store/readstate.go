package store

import (
	"errors"

	"jmessage/internal/model"
)

// Read/delivered state is one watermark record per (user, conversation)
// — read:<uid>:<cid> — instead of a key per (message, user). A message
// is delivered/read by a user iff their watermark >= its seq. Updates
// are monotonic; regressions (e.g. a reconnecting device replaying an
// old position) are ignored.

// GetReadState returns the user's watermarks for a conversation
// (zero-valued if none recorded yet).
func (s *Store) GetReadState(uid, cid string) (model.ReadState, error) {
	var rs model.ReadState
	err := s.getJSON(readStateKey(uid, cid), &rs)
	if errors.Is(err, ErrNotFound) {
		return model.ReadState{}, nil
	}
	return rs, err
}

// SetDelivered raises the user's delivered watermark. Returns the
// updated state and whether anything changed (callers broadcast
// receipts only on change).
func (s *Store) SetDelivered(uid, cid string, seq uint64) (model.ReadState, bool, error) {
	return s.updateReadState(uid, cid, func(rs *model.ReadState) bool {
		if seq <= rs.DeliveredSeq {
			return false
		}
		rs.DeliveredSeq = seq
		return true
	})
}

// SetRead raises the user's read watermark (and the delivered watermark
// with it — a read message was necessarily delivered).
func (s *Store) SetRead(uid, cid string, seq uint64) (model.ReadState, bool, error) {
	return s.updateReadState(uid, cid, func(rs *model.ReadState) bool {
		changed := false
		if seq > rs.ReadSeq {
			rs.ReadSeq = seq
			changed = true
		}
		if seq > rs.DeliveredSeq {
			rs.DeliveredSeq = seq
			changed = true
		}
		return changed
	})
}

// updateReadState serializes read-modify-write cycles across the whole
// store; watermark updates are tiny and infrequent enough that one
// mutex is simpler than per-key locking.
func (s *Store) updateReadState(uid, cid string, apply func(*model.ReadState) bool) (model.ReadState, bool, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	rs, err := s.GetReadState(uid, cid)
	if err != nil {
		return model.ReadState{}, false, err
	}
	if !apply(&rs) {
		return rs, false, nil
	}
	if err := s.putJSON(readStateKey(uid, cid), &rs); err != nil {
		return model.ReadState{}, false, err
	}
	return rs, true, nil
}

// ListReadStates returns the watermarks of the given members in a
// conversation (zero-valued entries for members with none recorded).
func (s *Store) ListReadStates(cid string, memberIDs []string) (map[string]model.ReadState, error) {
	out := make(map[string]model.ReadState, len(memberIDs))
	for _, uid := range memberIDs {
		rs, err := s.GetReadState(uid, cid)
		if err != nil {
			return nil, err
		}
		out[uid] = rs
	}
	return out, nil
}
