package tlimiter

import (
	"context"
	"time"
)

const (
	// DefaultPrefix is the default prefix applied to store keys.
	DefaultPrefix = "limiter"

	// DefaultMaxRetry is retained for API compatibility with store implementations
	// that may perform optimistic retries.
	DefaultMaxRetry = 3

	// DefaultCleanUpInterval is retained for API compatibility with store
	// implementations that perform periodic cleanup.
	DefaultCleanUpInterval = 30 * time.Second
)

// Context describes the observable rate-limit state for a key at a particular
// point in time.
type Context struct {
	// Limit is the maximum number of events permitted within the active window.
	Limit int64
	// Remaining is the number of events still allowed before the limit is reached.
	Remaining int64
	// Reset is the Unix timestamp, in seconds, at which the current window resets.
	Reset int64
	// Reached reports whether the current operation must be rejected. Get and Increment
	// reject counts above Limit; Peek reports whether the next event would exceed Limit.
	Reached bool
}

// GetContextFromState constructs a snapshot from an event count, a [Rate], and
// the window expiration time. Reached indicates whether the next event would exceed
// the limit, as in Peek. Incrementing operations must instead compare their final
// count with Limit using a strict greater-than comparison. Remaining is clamped
// to zero once the count reaches the limit.
func GetContextFromState(rate Rate, expiration time.Time, count int64) Context {
	limit := rate.Limit
	remaining := int64(0)
	reached := true

	if count < limit {
		remaining = limit - count
		reached = false
	}

	reset := expiration.Unix()

	state := Context{
		Limit:     limit,
		Remaining: remaining,
		Reset:     reset,
		Reached:   reached,
	}

	return state
}

// Store defines persistence for per-key rate-limit counters.
type Store interface {
	// Get increments the counter for key by one. Reached reports whether that event exceeds Limit.
	Get(ctx context.Context, key string, rate Rate) (Context, error)

	// Peek returns the current [Context] for key without modifying the counter.
	Peek(ctx context.Context, key string, rate Rate) (Context, error)

	// Reset removes the counter for key and returns the resulting [Context].
	Reset(ctx context.Context, key string, rate Rate) (Context, error)

	// Increment adds count to the counter for key and returns the updated [Context].
	// Reached reports whether the resulting count exceeds Limit.
	// If count is negative, implementations should return an error
	// whose message is "tlimiter: increment count must be non-negative"; see the
	// package documentation for the stable error contract.
	Increment(ctx context.Context, key string, count int64, rate Rate) (Context, error)
}

// StoreOptions configures store behavior such as key prefixes and maintenance
// intervals.
type StoreOptions struct {
	// Prefix is prepended to each store key.
	Prefix string

	// Deprecated: MaxRetry is ignored. Current store operations are atomic.
	MaxRetry int

	// CleanUpInterval is reserved for store implementations that reclaim stale
	// entries in the background. RedisStore ignores this setting.
	CleanUpInterval time.Duration
}
