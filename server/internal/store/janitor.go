package store

import "time"

// PruneClientMsgs deletes clientmsg: idempotency entries older than
// maxAge and returns how many were removed. The entries only matter
// within a client's retry window; pruning after days keeps the keyspace
// bounded. A pruned entry's message is unaffected (a truly ancient
// retry would duplicate, but clients drop their outbox on ack).
func (s *Store) PruneClientMsgs(maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge).UnixMilli()
	prefix := []byte(clientMsgPrefix)

	var stale [][]byte
	it, err := s.db.Scan(prefix, prefixEnd(prefix))
	if err != nil {
		return 0, err
	}
	for ; it.Valid(); it.Next() {
		var ref clientMsgRef
		if err := jsonUnmarshal(it.Value(), &ref); err != nil || ref.TS < cutoff {
			stale = append(stale, append([]byte(nil), it.Key()...))
		}
	}
	scanErr := it.Err()
	it.Close() // close before deleting below
	if scanErr != nil {
		return 0, scanErr
	}

	for _, k := range stale {
		if err := s.db.Delete(k); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}
