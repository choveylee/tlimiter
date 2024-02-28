/**
 * @Author: lidonglin
 * @Description:
 * @File:  store.go
 * @Version: 1.0.0
 * @Date: 2024/02/28 10:36
 */

package tlimiter

import (
	"context"
	"time"
)

const (
	// DefaultPrefix is the default prefix to use for the key in the store.
	DefaultPrefix = "limiter"

	// DefaultMaxRetry is the default maximum number of key retries under
	// race condition (mainly used with database-based stores).
	DefaultMaxRetry = 3

	// DefaultCleanUpInterval is the default time duration for cleanup.
	DefaultCleanUpInterval = 30 * time.Second
)

// Context is the limit context.
type Context struct {
	Limit     int64
	Remaining int64
	Reset     int64
	Reached   bool
}

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

// Store is the common interface for limiter stores.
type Store interface {
	// Get returns the limit for given identifier.
	Get(ctx context.Context, key string, rate Rate) (Context, error)

	// Peek returns the limit for given identifier, without modification on current values.
	Peek(ctx context.Context, key string, rate Rate) (Context, error)

	// Reset resets the limit to zero for given identifier.
	Reset(ctx context.Context, key string, rate Rate) (Context, error)

	// Increment increments the limit by given count & gives back the new limit for given identifier
	Increment(ctx context.Context, key string, count int64, rate Rate) (Context, error)
}

// StoreOptions are options for store.
type StoreOptions struct {
	// Prefix is the prefix to use for the key.
	Prefix string

	// MaxRetry is the maximum number of retry under race conditions on redis store.
	// Deprecated: this option is no longer required since all operations are atomic now.
	MaxRetry int

	// CleanUpInterval is the interval for cleanup (run garbage collection) on stale entries on memory store.
	// Setting this to a low value will optimize memory consumption, but will likely
	// reduce performance and increase lock contention.
	// Setting this to a high value will maximum throughput, but will increase the memory footprint.
	CleanUpInterval time.Duration
}
