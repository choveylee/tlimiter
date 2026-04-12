package tlimiter

import (
	"context"
	"errors"
)

// Limiter applies a [Rate] to keys using a [Store] and optional HTTP client-IP [Options].
type Limiter struct {
	Store   Store
	Rate    Rate
	Options Options
}

// NewLimiter returns a [Limiter] configured with store, rate, and optional functional options.
// It returns an error if store is nil or if rate has a non-positive period or limit; see package documentation for message strings.
func NewLimiter(store Store, rate Rate, options ...Option) (*Limiter, error) {
	if store == nil {
		return nil, errors.New("tlimiter: store is nil")
	}

	if rate.Period <= 0 || rate.Limit <= 0 {
		return nil, errors.New("tlimiter: rate period and limit must be positive")
	}

	opt := Options{
		IPv4Mask:           DefaultIPv4Mask,
		IPv6Mask:           DefaultIPv6Mask,
		TrustForwardHeader: false,
	}

	for _, o := range options {
		o(&opt)
	}

	return &Limiter{
		Store:   store,
		Rate:    rate,
		Options: opt,
	}, nil
}

// Get increments the counter for key by one and returns the updated [Context].
func (limiter *Limiter) Get(ctx context.Context, key string) (Context, error) {
	return limiter.Store.Get(ctx, key, limiter.Rate)
}

// Peek returns the current [Context] for key without changing the counter.
func (limiter *Limiter) Peek(ctx context.Context, key string) (Context, error) {
	return limiter.Store.Peek(ctx, key, limiter.Rate)
}

// Reset clears the counter for key and returns the new [Context].
func (limiter *Limiter) Reset(ctx context.Context, key string) (Context, error) {
	return limiter.Store.Reset(ctx, key, limiter.Rate)
}

// Increment adds count to the counter for key and returns the updated [Context].
// Invalid count values are rejected by the [Store] (see [Store.Increment]).
func (limiter *Limiter) Increment(ctx context.Context, key string, count int64) (Context, error) {
	return limiter.Store.Increment(ctx, key, count, limiter.Rate)
}
