# tlimiter

`tlimiter` is a Go rate-limiting library with pluggable storage backends and
helpers for deriving client IP keys from HTTP requests.

## Features

- Configurable fixed-window rate limits expressed as `Rate{Period, Limit}`
- Pluggable storage through the `Store` interface
- Redis-backed storage with atomic Lua-script operations
- Optional client IP masking and trusted proxy header handling

## Requirements

Go 1.27.4 or later. With `GOTOOLCHAIN=auto`, the Go command can download the
required toolchain automatically.

## Installation

```bash
go get github.com/choveylee/tlimiter
```

## Quick Start

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/choveylee/tlimiter"
	"github.com/redis/go-redis/v9"
)

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr:                  "127.0.0.1:6379",
		ContextTimeoutEnabled: true,
	})

	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := tlimiter.NewRedisStoreWithContext(ctx, rdb, tlimiter.StoreOptions{
		Prefix: tlimiter.DefaultPrefix,
	})
	if err != nil {
		log.Fatal(err)
	}

	limiter, err := tlimiter.NewLimiter(store, tlimiter.Rate{
		Period: time.Minute,
		Limit:  100,
	})
	if err != nil {
		log.Fatal(err)
	}

	state, err := limiter.Get(ctx, "user:42")
	if err != nil {
		log.Fatal(err)
	}

	if state.Reached {
		log.Printf("limit reached; retry after %d", state.Reset)
		return
	}

	log.Printf("request accepted; remaining=%d", state.Remaining)
}
```

## Redis Store

`RedisStore` uses Lua scripts to keep increment and peek operations atomic.
`NewRedisStoreWithContext` propagates cancellation while loading scripts. All
constructors set a five-second initialization context deadline or an earlier
caller deadline. The supplied Redis client must honor context deadlines to
bound socket I/O.
Data operations require a non-nil context, a positive period and a positive limit.

Create go-redis clients with `ContextTimeoutEnabled: true`. Constructors reject
standalone, cluster and ring clients without this setting. `tdb.NewRedisClient`
and `tdb.NewRedisClientEx` enable it. Custom `Client` implementations must honor
context deadlines. Keep socket read/write deadline handling enabled; the store
uses the caller's client without modifying its options or owning a separate pool.
In-flight socket I/O uses deadlines and configured timeouts; cancellation without
a deadline does not immediately interrupt an in-flight socket operation.

`Get` and `Increment` reject only counts greater than `Limit`: exactly `Limit`
events can be admitted. `Peek` reports whether the next event would be rejected,
so it reports `Reached` when the current count equals `Limit`. `Increment(0)`
does not extend an existing window. Counters without an expiration acquire one
when incremented. Lua returns counters as strings so Go retains full `int64`
precision. Lua cache eviction is retried once after reloading scripts; immutable
script digests allow concurrent requests to proceed without waiting on a reload
lock.
Reset timestamps are calculated from whole seconds and remaining milliseconds,
so long Redis TTL values do not overflow a nanosecond `time.Duration`.

`StoreOptions.Prefix` is honored when constructing Redis keys. The
`StoreOptions.MaxRetry` and `StoreOptions.CleanUpInterval` fields are accepted
for API compatibility but are not used by `RedisStore`.

## Client IP Resolution

`Limiter.GetIP`, `Limiter.GetIPWithMask`, and `Limiter.GetIPKey` can derive a
store key from the request's client IP address.

- `WithTrustForwardHeader(true)` enables `X-Forwarded-For` and `X-Real-IP`
- `WithClientIPHeader(name)` gives precedence to a specific header
- `WithIPv4Mask` and `WithIPv6Mask` control address masking before key creation

Enable header-based IP resolution only when an upstream reverse proxy
sanitizes or overwrites those headers. Otherwise, clients may spoof their
apparent address.

## Stable Error Messages

The following errors are documented as stable and may be matched by exact
string value:

- `tlimiter: store must not be nil`
- `tlimiter: rate period and limit must both be positive`
- `tlimiter: redis client must not be nil`
- `tlimiter: increment count must be non-negative`
- `tlimiter: context must not be nil`
- `tlimiter: option must not be nil`
- `tlimiter: IPv4 mask must contain 4 or 16 bytes`
- `tlimiter: IPv6 mask must contain 16 bytes`

## Validation

```bash
go build ./...
go vet ./...
```

## License

No license file is currently included in this repository.
