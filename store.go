package tlimiter

import (
	"context"
	"time"
)

const (
	// DefaultPrefix is the default key prefix for store entries.
	DefaultPrefix = "limiter"

	// DefaultMaxRetry is the default retry count under contention for database-oriented stores.
	DefaultMaxRetry = 3

	// DefaultCleanUpInterval is the default garbage-collection interval for in-memory stores.
	DefaultCleanUpInterval = 30 * time.Second
)

// Context is the observable rate-limit state for a key at a point in time.
type Context struct {
	// Limit is the maximum permitted events in the window (from [Rate.Limit]).
	Limit int64
	// Remaining is the number of events still allowed before the limit is reached.
	Remaining int64
	// Reset is the Unix time when the window resets (seconds since epoch).
	Reset int64
	// Reached reports whether the current count has met or exceeded Limit.
	Reached bool
}

// GetContextFromState constructs a [Context] from the event count, [Rate], and window expiration instant.
func GetContextFromState(rate Rate, expiration time.Time, count int64) Context {
	limit := rate.Limit
	remaining := int64(0)
	reached := true

	if count <= limit {
		remaining = limit - count
		reached = false
	}

	reset := expiration.Unix()

	return Context{
		Limit:     limit,
		Remaining: remaining,
		Reset:     reset,
		Reached:   reached,
	}
}

// Store defines persistence for per-key rate limit counters.
type Store interface {
	// Get increments the counter for key by one and returns the updated [Context].
	Get(ctx context.Context, key string, rate Rate) (Context, error)

	// Peek returns the current [Context] for key without modifying the counter.
	Peek(ctx context.Context, key string, rate Rate) (Context, error)

	// Reset removes the counter for key and returns the resulting [Context].
	Reset(ctx context.Context, key string, rate Rate) (Context, error)

	// Increment adds count to the counter for key and returns the updated [Context].
	// If count is negative, implementations should return an error whose message is
	// "tlimiter: increment count must not be negative" (see package documentation).
	Increment(ctx context.Context, key string, count int64, rate Rate) (Context, error)
}

// StoreOptions configures store behavior such as key prefix and maintenance intervals.
type StoreOptions struct {
	// Prefix is the prefix to use for the key.
	Prefix string

	// Deprecated: MaxRetry is ignored; store operations are atomic.
	MaxRetry int

	// CleanUpInterval controls how often a memory-backed store may reclaim stale entries.
	// Lower values reduce memory use but may increase contention; higher values favor throughput.
	CleanUpInterval time.Duration
}
