package ws

import (
	"sync"
	"time"
)

// tokenBucket is a per-connection rate limiter: refills continuously at
// refillPerSec, caps at capacity (the burst allowance). Used to stop a
// single client — buggy or malicious — from flooding the hub with
// frames faster than a human (or reasonable client) ever would.
type tokenBucket struct {
	capacity     float64
	refillPerSec float64

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newTokenBucket(capacity, refillPerSec float64) *tokenBucket {
	return &tokenBucket{capacity: capacity, refillPerSec: refillPerSec, tokens: capacity, last: time.Now()}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.refillPerSec
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
