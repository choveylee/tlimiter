package tlimiter

import (
	"context"
	"errors"
	"net"
	"reflect"
	"slices"
	"strings"
)

// Limiter enforces a [Rate] for application-defined keys by using a [Store]
// and optional client IP resolution [Options].
type Limiter struct {
	Store   Store
	Rate    Rate
	Options Options
}

// NewLimiter constructs a [Limiter] from a store, a rate, and optional
// functional options.
//
// It rejects nil stores (including typed nil), nil options, unusable IP masks,
// and non-positive rate periods or limits. The package documentation lists stable errors
// returned by this function. IP masks are copied so later changes to caller-owned
// slices do not alter this limiter's configuration.
func NewLimiter(store Store, rate Rate, options ...Option) (*Limiter, error) {
	if isNil(store) {
		return nil, errors.New("tlimiter: store must not be nil")
	}

	if rate.Period <= 0 || rate.Limit <= 0 {
		return nil, errors.New("tlimiter: rate period and limit must both be positive")
	}

	limiterOptions := Options{
		IPv4Mask:           slices.Clone(DefaultIPv4Mask),
		IPv6Mask:           slices.Clone(DefaultIPv6Mask),
		TrustForwardHeader: false,
	}

	for _, option := range options {
		if option == nil {
			return nil, errors.New("tlimiter: option must not be nil")
		}

		option(&limiterOptions)
	}

	if net.IPv4zero.Mask(limiterOptions.IPv4Mask) == nil {
		return nil, errors.New("tlimiter: IPv4 mask must contain 4 or 16 bytes")
	}

	if net.IPv6zero.Mask(limiterOptions.IPv6Mask) == nil {
		return nil, errors.New("tlimiter: IPv6 mask must contain 16 bytes")
	}

	limiterOptions.ClientIPHeader = strings.TrimSpace(limiterOptions.ClientIPHeader)
	limiterOptions.IPv4Mask = slices.Clone(limiterOptions.IPv4Mask)
	limiterOptions.IPv6Mask = slices.Clone(limiterOptions.IPv6Mask)

	limiter := &Limiter{
		Store:   store,
		Rate:    rate,
		Options: limiterOptions,
	}

	return limiter, nil
}

// Get increments the counter for key by one and returns the updated [Context].
// Reached reports whether this event exceeds the limit; the event at Limit is allowed.
func (limiter *Limiter) Get(ctx context.Context, key string) (Context, error) {
	return limiter.Store.Get(ctx, key, limiter.Rate)
}

// Peek returns the current [Context] for key without changing the counter.
// Reached reports whether another event would exceed the limit.
func (limiter *Limiter) Peek(ctx context.Context, key string) (Context, error) {
	return limiter.Store.Peek(ctx, key, limiter.Rate)
}

// Reset clears the counter for key and returns the resulting [Context].
func (limiter *Limiter) Reset(ctx context.Context, key string) (Context, error) {
	return limiter.Store.Reset(ctx, key, limiter.Rate)
}

// Increment adds count to the counter for key and returns the updated [Context].
// Invalid count values are rejected by the [Store]; see [Store.Increment].
func (limiter *Limiter) Increment(ctx context.Context, key string, count int64) (Context, error) {
	return limiter.Store.Increment(ctx, key, count, limiter.Rate)
}

// isNil also recognizes nil concrete values stored in an interface.
func isNil(value interface{}) bool {
	if value == nil {
		return true
	}

	reflectedValue := reflect.ValueOf(value)
	switch reflectedValue.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return reflectedValue.IsNil()
	default:
		return false
	}
}
