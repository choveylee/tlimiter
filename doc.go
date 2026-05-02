// Package tlimiter provides rate limiting with pluggable storage backends and
// helpers for resolving client IP addresses from HTTP requests.
//
// # Limiter
//
// Use [NewLimiter] to construct a [Limiter]. A limiter combines a [Store], a
// [Rate], and optional [Options] for client IP masking and trusted proxy
// header handling.
//
// # Storage
//
// The [Store] interface defines persistence for per-key counters. The package
// includes [RedisStore], a Redis-backed implementation that uses Lua scripts
// for atomic increment and peek operations.
//
// # Reverse proxies and client IP
//
// [WithTrustForwardHeader] and [WithClientIPHeader] allow client addresses to
// be read from HTTP headers. These options should be enabled only when an
// upstream reverse proxy overwrites or sanitizes the corresponding headers;
// otherwise, clients may be able to spoof the resolved IP address.
//
// # Errors
//
// The following operations return stable error messages that are safe to match
// by exact string value:
//
//   - [NewLimiter]: "tlimiter: store must not be nil" if [Store] is nil.
//   - [NewLimiter]: "tlimiter: rate period and limit must both be positive" if [Rate] is invalid.
//   - [NewRedisStore] and [NewRedisStoreWithOptions]: "tlimiter: redis client must not be nil" if the Redis [Client] is nil.
//   - [RedisStore.Increment] and compatible [Store] implementations: "tlimiter: increment count must be non-negative" if count is negative.
package tlimiter
