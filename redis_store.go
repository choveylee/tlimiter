package tlimiter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisInitTimeout = 5 * time.Second

	luaIncrScript = `
local key = KEYS[1]
local ttl = redis.call("pttl", key)
redis.call("incrby", key, ARGV[1])
local count = redis.call("get", key)
if ttl < 0 then
 ttl = tonumber(ARGV[2])
 redis.call("pexpire", key, ARGV[2])
end
return {count, ttl}
`
	luaPeekScript = `
local key = KEYS[1]
local v = redis.call("get", key)
if v == false then
	return {"0", -2}
end
local ttl = redis.call("pttl", key)
return {v, ttl}
`
)

// Client is the minimal Redis client surface required by [RedisStore]. Both
// standalone and cluster clients may satisfy this interface.
// Implementations must honor context deadlines; go-redis clients must enable
// ContextTimeoutEnabled when constructed.
type Client interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Watch(ctx context.Context, handler func(*redis.Tx) error, keys ...string) error
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	EvalSha(ctx context.Context, sha string, keys []string, args ...interface{}) *redis.Cmd
	ScriptLoad(ctx context.Context, script string) *redis.StringCmd
}

// RedisStore is a [Store] implementation backed by Redis Lua scripts.
// Each key's fixed window starts on its first increment; subsequent increments
// preserve the expiration until the window ends, including rejected events.
type RedisStore struct {
	// Prefix is prepended to each Redis key.
	Prefix string

	// client is the Redis client used for commands.
	client Client

	// Script digests are computed once and remain immutable during reloads.
	luaIncrSHA string
	luaPeekSHA string
}

// NewRedisStore constructs a Redis-backed [Store] with [DefaultPrefix].
//
// [DefaultCleanUpInterval] is passed through for API compatibility but is not
// used by [RedisStore].
func NewRedisStore(client Client) (Store, error) {
	options := StoreOptions{
		Prefix:          DefaultPrefix,
		CleanUpInterval: DefaultCleanUpInterval,
	}

	return NewRedisStoreWithOptions(client, options)
}

// NewRedisStoreWithOptions constructs a Redis-backed [Store] from the supplied
// client and [StoreOptions].
//
// [StoreOptions.Prefix] is honored. [StoreOptions.MaxRetry] and
// [StoreOptions.CleanUpInterval] are accepted for API compatibility but are not
// used by [RedisStore].
func NewRedisStoreWithOptions(client Client, options StoreOptions) (Store, error) {
	return NewRedisStoreWithContext(context.Background(), client, options)
}

// NewRedisStoreWithContext constructs a Redis store and loads its scripts using ctx.
// The initialization context has a five-second deadline, or the earlier deadline
// on ctx. The supplied Redis client must honor deadlines to bound socket I/O.
// Go-redis clients with ContextTimeoutEnabled disabled are rejected.
// Set options.Prefix to DefaultPrefix to use the default key namespace.
func NewRedisStoreWithContext(ctx context.Context, client Client, options StoreOptions) (Store, error) {
	if ctx == nil {
		return nil, errors.New("tlimiter: context must not be nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if isNil(client) {
		return nil, errors.New("tlimiter: redis client must not be nil")
	}

	// Shared client options are read-only; validate deadline support before I/O.
	contextTimeoutEnabled := true
	switch redisClient := client.(type) {
	case interface{ Options() *redis.Options }:
		clientOptions := redisClient.Options()
		contextTimeoutEnabled = clientOptions != nil && clientOptions.ContextTimeoutEnabled
	case interface{ Options() *redis.ClusterOptions }:
		clientOptions := redisClient.Options()
		contextTimeoutEnabled = clientOptions != nil && clientOptions.ContextTimeoutEnabled
	case interface{ Options() *redis.RingOptions }:
		clientOptions := redisClient.Options()
		contextTimeoutEnabled = clientOptions != nil && clientOptions.ContextTimeoutEnabled
	}

	if !contextTimeoutEnabled {
		return nil, errors.New("tlimiter: redis client context timeouts must be enabled")
	}

	store := &RedisStore{
		client:     client,
		Prefix:     options.Prefix,
		luaIncrSHA: redis.NewScript(luaIncrScript).Hash(),
		luaPeekSHA: redis.NewScript(luaPeekScript).Hash(),
	}

	ctx, cancel := context.WithTimeout(ctx, defaultRedisInitTimeout)
	defer cancel()

	err := store.loadLuaScripts(ctx)
	if err != nil {
		return nil, err
	}

	return store, nil
}

// Get increments key by one and reports whether the updated count exceeds Limit.
// Rejected events remain counted in the active window.
func (store *RedisStore) Get(ctx context.Context, key string, rate Rate) (Context, error) {
	return store.Increment(ctx, key, 1, rate)
}

// Peek reads key's count and remaining TTL without consuming quota or starting a window.
// Reached reports whether the next event would exceed Limit.
func (store *RedisStore) Peek(ctx context.Context, key string, rate Rate) (Context, error) {
	if err := validateStoreRequest(ctx, rate); err != nil {
		return Context{}, err
	}

	cmd := store.evalSHA(ctx, store.luaPeekSHA, []string{store.getCacheKey(key)})

	count, ttl, err := parseCountAndTTL(cmd)
	if err != nil {
		return Context{}, err
	}

	curTime := time.Now()

	expiration := curTime.Add(rate.Period)
	if ttl >= 0 {
		// Split the TTL so long windows cannot overflow a nanosecond duration.
		expiration = time.Unix(
			curTime.Unix()+ttl/1000,
			int64(curTime.Nanosecond())+(ttl%1000)*int64(time.Millisecond),
		)
	}

	return GetContextFromState(rate, expiration, count), nil
}

// Reset removes key's counter without consuming quota. The next increment starts
// a new window; the returned snapshot reports the full quota as available.
func (store *RedisStore) Reset(ctx context.Context, key string, rate Rate) (Context, error) {
	if err := validateStoreRequest(ctx, rate); err != nil {
		return Context{}, err
	}

	_, err := store.client.Del(ctx, store.getCacheKey(key)).Result()
	if err != nil {
		return Context{}, fmt.Errorf("tlimiter: reset Redis counter: %w", err)
	}

	count := int64(0)

	curTime := time.Now()

	expiration := curTime.Add(rate.Period)

	return GetContextFromState(rate, expiration, count), nil
}

// Increment atomically adds count and preserves the active window's expiration.
// A new or persistent counter receives a TTL based on rate.Period. Counts above
// Limit are retained and reported as reached; a count equal to Limit is allowed.
// A negative count returns an error; see the package documentation for its stable message.
func (store *RedisStore) Increment(ctx context.Context, key string, count int64, rate Rate) (Context, error) {
	if err := validateStoreRequest(ctx, rate); err != nil {
		return Context{}, err
	}

	if count < 0 {
		return Context{}, errors.New("tlimiter: increment count must be non-negative")
	}

	// Round up to Redis milliseconds so a sub-millisecond window still expires.
	milliseconds := rate.Period / time.Millisecond
	if rate.Period%time.Millisecond != 0 {
		milliseconds++
	}

	cmd := store.evalSHA(ctx, store.luaIncrSHA, []string{store.getCacheKey(key)}, count, int64(milliseconds))

	currentCount, ttl, err := parseCountAndTTL(cmd)
	if err != nil {
		return Context{}, err
	}

	curTime := time.Now()

	expiration := curTime.Add(rate.Period)
	if ttl >= 0 {
		// Split the TTL so long windows cannot overflow a nanosecond duration.
		expiration = time.Unix(
			curTime.Unix()+ttl/1000,
			int64(curTime.Nanosecond())+(ttl%1000)*int64(time.Millisecond),
		)
	}

	state := GetContextFromState(rate, expiration, currentCount)
	// The event at Limit is admitted; Peek instead checks the next event.
	state.Reached = currentCount > rate.Limit

	return state, nil
}

// evalSHA executes EVALSHA using the immutable script SHA. If Redis
// returns NOSCRIPT, the scripts are reloaded and the command is retried once.
// NOSCRIPT means the script did not execute, so this recovery does not consume
// quota twice. Other errors are returned without a store-level retry.
func (store *RedisStore) evalSHA(ctx context.Context, sha string, keys []string, args ...interface{}) *redis.Cmd {
	cmd := store.client.EvalSha(ctx, sha, keys, args...)

	err := cmd.Err()
	if err == nil || !strings.HasPrefix(err.Error(), "NOSCRIPT") {
		return cmd
	}

	err = store.loadLuaScripts(ctx)
	if err != nil {
		cmd = redis.NewCmd(ctx)
		cmd.SetErr(err)

		return cmd
	}

	return store.client.EvalSha(ctx, sha, keys, args...)
}

// loadLuaScripts loads both scripts for initialization or NOSCRIPT recovery.
// SCRIPT LOAD is idempotent, so concurrent reloads need no shared lock across I/O.
func (store *RedisStore) loadLuaScripts(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	err := store.client.ScriptLoad(ctx, luaIncrScript).Err()
	if err != nil {
		return fmt.Errorf("tlimiter: load increment Lua script: %w", err)
	}

	err = store.client.ScriptLoad(ctx, luaPeekScript).Err()
	if err != nil {
		return fmt.Errorf("tlimiter: load peek Lua script: %w", err)
	}

	return nil
}

// getCacheKey constructs the Redis key for an application-level key.
func (store *RedisStore) getCacheKey(key string) string {
	return store.Prefix + ":" + key
}

// parseCountAndTTL parses the {count-string, ttl-ms} pair returned by the scripts.
// Counts stay as strings until Go parses them, preserving the full int64 range.
func parseCountAndTTL(cmd *redis.Cmd) (int64, int64, error) {
	result, err := cmd.Result()
	if err != nil {
		return 0, 0, fmt.Errorf("tlimiter: execute Redis Lua script: %w", err)
	}

	fields, ok := result.([]interface{})
	if !ok || len(fields) != 2 {
		return 0, 0, errors.New("tlimiter: Lua result must be an array with 2 elements")
	}

	countString, ok := fields[0].(string)
	if !ok {
		return 0, 0, errors.New("tlimiter: Lua count must be an integer string")
	}

	count, err := strconv.ParseInt(countString, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("tlimiter: parse Lua count: %w", err)
	}

	ttl, ok := fields[1].(int64)
	if !ok {
		return 0, 0, errors.New("tlimiter: Lua TTL must be an integer")
	}

	return count, ttl, nil
}

// validateStoreRequest rejects unusable contexts and rates before any Redis command.
func validateStoreRequest(ctx context.Context, rate Rate) error {
	if ctx == nil {
		return errors.New("tlimiter: context must not be nil")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if rate.Period <= 0 || rate.Limit <= 0 {
		return errors.New("tlimiter: rate period and limit must both be positive")
	}

	return nil
}
