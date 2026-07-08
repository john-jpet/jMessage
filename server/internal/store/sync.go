package store

import "jmessage/internal/model"

// maxSyncBatch bounds one sync batch per conversation; the client
// repeats sync (or uses history paging) when hasMore is set.
const maxSyncBatch = 200

// ListMessagesAfter returns up to limit messages with seq > after,
// ascending, plus whether more remain. This is the sync primitive:
// "I have through seq N, give me what follows" is a natural forward
// scan on the dense message keys — no reverse iteration, no timestamps.
func (s *Store) ListMessagesAfter(cid string, after uint64, limit int) (msgs []model.Message, hasMore bool, err error) {
	if limit <= 0 || limit > maxSyncBatch {
		limit = maxSyncBatch
	}
	it, err := s.db.Scan(msgKey(cid, after+1), prefixEnd(msgPrefix(cid)))
	if err != nil {
		return nil, false, err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		if len(msgs) == limit {
			return msgs, true, nil
		}
		var m model.Message
		if err := jsonUnmarshal(it.Value(), &m); err != nil {
			return nil, false, err
		}
		msgs = append(msgs, m)
	}
	return msgs, false, it.Err()
}
