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
	// Reached reports whether the current count has met or exceeded [Context.Limit].
	Reached bool
}

// GetContextFromState constructs a [Context] from an event count, a [Rate], and
// the window expiration time.
func GetContextFromState(rate Rate, expiration time.Time, count int64) Context {
	limit := rate.Limit
	remaining := int64(0)
	reached := true

	if count < limit {
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

// Store defines persistence for per-key rate-limit counters.
type Store interface {
	// Get increments the counter for key by one and returns the updated [Context].
	Get(ctx context.Context, key string, rate Rate) (Context, error)

	// Peek returns the current [Context] for key without modifying the counter.
	Peek(ctx context.Context, key string, rate Rate) (Context, error)

	// Reset removes the counter for key and returns the resulting [Context].
	Reset(ctx context.Context, key string, rate Rate) (Context, error)

	// Increment adds count to the counter for key and returns the updated
	// [Context]. If count is negative, implementations should return an error
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
