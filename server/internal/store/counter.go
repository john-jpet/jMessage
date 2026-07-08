package store

import (
	"errors"
	"fmt"
	"strconv"
	"sync"

	petdb "petdb"
)

// counter allocates global IDs (users, conversations) crash-safely
// without transactions: the persisted value is only a hint, refreshed
// after each allocation's entity writes. On open, the true maximum is
// recovered by scanning the entity keyspace above the hint, so a crash
// between "write entity" and "write hint" can never reuse an ID.
type counter struct {
	mu        sync.Mutex
	next      uint64
	persisted uint64
	key       []byte
	db        *petdb.DB
}

// openCounter loads the hint and reconciles it against existing entity
// keys under entityPrefix (e.g. "user:"), whose final segment is the
// zero-padded ID.
func openCounter(db *petdb.DB, hintKey, entityPrefix []byte) (*counter, error) {
	c := &counter{key: hintKey, db: db}

	var hint uint64
	if v, err := db.Get(hintKey); err == nil {
		hint, _ = strconv.ParseUint(string(v), 10, 64)
	} else if !errors.Is(err, petdb.ErrNotFound) {
		return nil, fmt.Errorf("counter %s: %w", hintKey, err)
	}

	// Scan entities with ID > hint; the hint is stale by at most the few
	// allocations in flight at crash time.
	max := hint
	start := append(append([]byte(nil), entityPrefix...), pad10(hint+1)...)
	it, err := db.Scan(start, prefixEnd(entityPrefix))
	if err != nil {
		return nil, err
	}
	defer it.Close()
	for ; it.Valid(); it.Next() {
		if id, err := strconv.ParseUint(lastSegment(it.Key()), 10, 64); err == nil && id > max {
			max = id
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}

	c.next = max + 1
	c.persisted = hint
	return c, nil
}

// alloc reserves the next ID. The caller must write the entity keys and
// then call persist(id); a crash in between is healed by the open scan.
func (c *counter) alloc() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.next
	c.next++
	return id
}

// persist refreshes the durable hint (monotonically — concurrent
// allocations may complete out of order).
func (c *counter) persist(id uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id <= c.persisted {
		return nil
	}
	if err := c.db.Put(c.key, []byte(pad10(id))); err != nil {
		return err
	}
	c.persisted = id
	return nil
}
