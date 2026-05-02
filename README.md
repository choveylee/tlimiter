# tlimiter

`tlimiter` is a Go rate-limiting library with pluggable storage backends and
helpers for deriving client IP keys from HTTP requests.

## Features

- Configurable fixed-window rate limits expressed as `Rate{Period, Limit}`
- Pluggable storage through the `Store` interface
- Redis-backed storage with atomic Lua-script operations
- Optional client IP masking and trusted proxy header handling

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
		Addr: "127.0.0.1:6379",
	})

	store, err := tlimiter.NewRedisStore(rdb)
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

	ctx, err := limiter.Get(context.Background(), "user:42")
	if err != nil {
		log.Fatal(err)
	}

	if ctx.Reached {
		log.Printf("limit reached; retry after %d", ctx.Reset)
		return
	}

	log.Printf("request accepted; remaining=%d", ctx.Remaining)
}
```

## Redis Store

`RedisStore` uses Lua scripts to keep increment and peek operations atomic.
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

## License

No license file is currently included in this repository.
