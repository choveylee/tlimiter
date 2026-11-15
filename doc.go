// Package tlimiter provides rate limiting with pluggable storage backends and
// helpers for resolving client IP addresses from HTTP requests.
//
// Constructors reject nil stores/clients (including typed nil), nil options and
// unusable IP masks. Redis initialization receives a five-second context deadline;
// use [NewRedisStoreWithContext] to propagate an earlier deadline or cancellation.
// The supplied Redis client must honor context deadlines to bound socket I/O.
// Go-redis clients must enable ContextTimeoutEnabled; constructors reject them
// when this setting is disabled.
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
// for atomic increment and peek operations. Get and Increment admit exactly Limit
// events before reporting Reached; Peek reports whether the next event would be
// rejected. Incrementing by zero preserves the existing window expiration.
// Lua returns counters as strings to preserve int64 precision. Script digests
// are immutable, and cache-miss recovery does not hold a shared network lock.
// Reset timestamps use split second/millisecond arithmetic to avoid duration overflow.
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
//   - [NewLimiter]: "tlimiter: store must not be nil" if [Store] is nil, including typed nil.
//   - [NewLimiter]: "tlimiter: rate period and limit must both be positive" if [Rate] is invalid.
//   - [NewRedisStore] and [NewRedisStoreWithOptions]: "tlimiter: redis client must not be nil" if the Redis [Client] is nil.
//   - [NewLimiter]: "tlimiter: option must not be nil" if an option is nil.
//   - [NewLimiter]: "tlimiter: IPv4 mask must contain 4 or 16 bytes" or "tlimiter: IPv6 mask must contain 16 bytes" for unusable masks.
//   - Redis operations: "tlimiter: context must not be nil" for nil contexts.
//   - [RedisStore.Increment] and compatible [Store] implementations: "tlimiter: increment count must be non-negative" if count is negative.
package tlimiter
