// Package tlimiter provides rate limiting with pluggable storage backends and
// helpers for resolving client IP addresses from HTTP requests.
//
// # Limiter
//
// Construct a [Limiter] with [NewLimiter], which validates inputs and may return
// errors described in the “Errors” section below. The limiter combines a [Store],
// a [Rate], and optional [Options] for IP masking and HTTP proxy header handling.
//
// # Storage
//
// The [Store] interface defines per-key counters. [NewRedisStore] and
// [NewRedisStoreWithOptions] construct a [RedisStore] backed by Redis, using Lua
// scripts for atomic increment and peek operations.
//
// # Reverse proxies and client IP
//
// Options such as [WithTrustForwardHeader] and [WithClientIPHeader] read client
// addresses from HTTP headers. If clients can set those headers directly, the
// resolved IP may be spoofed unless a reverse proxy overwrites or strips them and
// forwards a trusted address. Configure the proxy before enabling these options.
//
// # Errors
//
// The following operations return errors whose messages are stable and documented here
// (for example err.Error() == "tlimiter: store is nil"):
//
//   - [NewLimiter]: "tlimiter: store is nil" if [Store] is nil; "tlimiter: rate period and limit must be positive" if [Rate] is invalid.
//   - [NewRedisStore] and [NewRedisStoreWithOptions]: "tlimiter: redis client is nil" if the Redis [Client] is nil.
//   - [RedisStore.Increment] (and other [Store] implementations should match): "tlimiter: increment count must not be negative" if count is negative.
package tlimiter
