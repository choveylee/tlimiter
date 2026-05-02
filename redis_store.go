package tlimiter

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
)

const (
	luaIncrScript = `
local key = KEYS[1]
local count = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local ret = redis.call("incrby", key, ARGV[1])
if ret == count then
	if ttl > 0 then
		redis.call("pexpire", key, ARGV[2])
	end
	return {ret, ttl}
end
ttl = redis.call("pttl", key)
return {ret, ttl}
`
	luaPeekScript = `
local key = KEYS[1]
local v = redis.call("get", key)
if v == false then
	return {0, 0}
end
local ttl = redis.call("pttl", key)
return {tonumber(v), ttl}
`
)

// Client is the minimal Redis client surface required by [RedisStore]. Both
// standalone and cluster clients may satisfy this interface.
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
type RedisStore struct {
	// Prefix is prepended to each Redis key.
	Prefix string

	// client is the Redis client used for commands.
	client Client

	// luaMutex protects the cached script SHA fields during load and concurrent reads.
	luaMutex sync.RWMutex
	// luaLoaded is non-zero after both Lua scripts have been loaded successfully.
	luaLoaded uint32
	// luaIncrSHA is the SHA1 digest of the increment script used by EVALSHA.
	luaIncrSHA string
	// luaPeekSHA is the SHA1 digest of the peek script used by EVALSHA.
	luaPeekSHA string
}

// NewRedisStore constructs a Redis-backed [Store] with [DefaultPrefix].
//
// [DefaultCleanUpInterval] is passed through for API compatibility but is not
// used by [RedisStore].
func NewRedisStore(client Client) (Store, error) {
	return NewRedisStoreWithOptions(client, StoreOptions{
		Prefix:          DefaultPrefix,
		CleanUpInterval: DefaultCleanUpInterval,
	})
}

// NewRedisStoreWithOptions constructs a Redis-backed [Store] from the supplied
// client and [StoreOptions].
//
// [StoreOptions.Prefix] is honored. [StoreOptions.MaxRetry] and
// [StoreOptions.CleanUpInterval] are accepted for API compatibility but are not
// used by [RedisStore].
func NewRedisStoreWithOptions(client Client, options StoreOptions) (Store, error) {
	if isNilClient(client) {
		return nil, errors.New("tlimiter: redis client must not be nil")
	}

	store := &RedisStore{
		client: client,
		Prefix: options.Prefix,
	}

	err := store.preloadLuaScripts(context.Background())
	if err != nil {
		return nil, err
	}

	return store, nil
}

// isNilClient reports whether c is an unusable [Client], including a typed nil
// pointer stored in an interface value.
func isNilClient(c Client) bool {
	if c == nil {
		return true
	}
	rv := reflect.ValueOf(c)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// Increment implements [Store.Increment]. A negative count returns an error;
// see the package documentation for the stable error message.
func (store *RedisStore) Increment(ctx context.Context, key string, count int64, rate Rate) (Context, error) {
	if count < 0 {
		return Context{}, errors.New("tlimiter: increment count must be non-negative")
	}
	cmd := store.evalSHA(ctx, store.getLuaIncrSHA, []string{store.getCacheKey(key)}, count, periodMilliseconds(rate.Period))

	return currentContext(cmd, rate)
}

// Get implements [Store.Get].
func (store *RedisStore) Get(ctx context.Context, key string, rate Rate) (Context, error) {
	cmd := store.evalSHA(ctx, store.getLuaIncrSHA, []string{store.getCacheKey(key)}, 1, periodMilliseconds(rate.Period))

	return currentContext(cmd, rate)
}

// Peek implements [Store.Peek].
func (store *RedisStore) Peek(ctx context.Context, key string, rate Rate) (Context, error) {
	cmd := store.evalSHA(ctx, store.getLuaPeekSHA, []string{store.getCacheKey(key)})

	count, ttl, err := parseCountAndTTL(cmd)
	if err != nil {
		return Context{}, err
	}

	curTime := time.Now()

	expiration := curTime.Add(rate.Period)
	if ttl > 0 {
		expiration = curTime.Add(time.Duration(ttl) * time.Millisecond)
	}

	return GetContextFromState(rate, expiration, count), nil
}

// Reset implements [Store.Reset].
func (store *RedisStore) Reset(ctx context.Context, key string, rate Rate) (Context, error) {
	_, err := store.client.Del(ctx, store.getCacheKey(key)).Result()
	if err != nil {
		return Context{}, err
	}

	count := int64(0)

	curTime := time.Now()

	expiration := curTime.Add(rate.Period)

	return GetContextFromState(rate, expiration, count), nil
}

// getCacheKey constructs the Redis key for an application-level key.
func (store *RedisStore) getCacheKey(key string) string {
	buffer := strings.Builder{}

	buffer.WriteString(store.Prefix)
	buffer.WriteString(":")
	buffer.WriteString(key)

	return buffer.String()
}

// preloadLuaScripts ensures that the Lua scripts are loaded at most once for
// the current process state.
func (store *RedisStore) preloadLuaScripts(ctx context.Context) error {
	// This follows the same fast-path pattern used by sync.Once.
	if atomic.LoadUint32(&store.luaLoaded) == 0 {
		return store.loadLuaScripts(ctx)
	}

	return nil
}

// reloadLuaScripts forces both Lua scripts to be reloaded, for example after a
// Redis NOSCRIPT error.
func (store *RedisStore) reloadLuaScripts(ctx context.Context) error {
	// Reset the loaded state before reloading. This follows the same pattern
	// used by sync.Once-style initialization guards.
	atomic.StoreUint32(&store.luaLoaded, 0)

	return store.loadLuaScripts(ctx)
}

// loadLuaScripts loads both scripts with SCRIPT LOAD and stores their SHA
// digests.
//
// Callers should use preloadLuaScripts or reloadLuaScripts rather than calling
// loadLuaScripts directly.
func (store *RedisStore) loadLuaScripts(ctx context.Context) error {
	store.luaMutex.Lock()
	defer store.luaMutex.Unlock()

	// Check again after acquiring the lock in case another goroutine completed
	// the load first.
	if atomic.LoadUint32(&store.luaLoaded) != 0 {
		return nil
	}

	luaIncrSHA, err := store.client.ScriptLoad(ctx, luaIncrScript).Result()
	if err != nil {
		return pkgerrors.Wrap(err, `tlimiter: failed to load "incr" Lua script`)
	}

	luaPeekSHA, err := store.client.ScriptLoad(ctx, luaPeekScript).Result()
	if err != nil {
		return pkgerrors.Wrap(err, `tlimiter: failed to load "peek" Lua script`)
	}

	store.luaIncrSHA = luaIncrSHA
	store.luaPeekSHA = luaPeekSHA

	atomic.StoreUint32(&store.luaLoaded, 1)

	return nil
}

// getLuaIncrSHA returns the cached increment script SHA under a read lock.
func (store *RedisStore) getLuaIncrSHA() string {
	store.luaMutex.RLock()
	defer store.luaMutex.RUnlock()

	return store.luaIncrSHA
}

// getLuaPeekSHA returns the cached peek script SHA under a read lock.
func (store *RedisStore) getLuaPeekSHA() string {
	store.luaMutex.RLock()
	defer store.luaMutex.RUnlock()

	return store.luaPeekSHA
}

// evalSHA executes EVALSHA by using the SHA returned by getSha. If Redis
// returns NOSCRIPT, the scripts are reloaded and the command is retried once.
func (store *RedisStore) evalSHA(ctx context.Context, getSha func() string,
	keys []string, args ...interface{}) *redis.Cmd {

	cmd := store.client.EvalSha(ctx, getSha(), keys, args...)

	err := cmd.Err()
	if err == nil || !isLuaScriptGone(err) {
		return cmd
	}

	err = store.reloadLuaScripts(ctx)
	if err != nil {
		cmd = redis.NewCmd(ctx)
		cmd.SetErr(err)

		return cmd
	}

	return store.client.EvalSha(ctx, getSha(), keys, args...)
}

// isLuaScriptGone reports whether err represents a Redis NOSCRIPT error.
func isLuaScriptGone(err error) bool {
	return strings.HasPrefix(err.Error(), "NOSCRIPT")
}

// parseCountAndTTL parses the {count, ttl-ms} pair returned by the Lua
// scripts.
func parseCountAndTTL(cmd *redis.Cmd) (int64, int64, error) {
	result, err := cmd.Result()
	if err != nil {
		return 0, 0, pkgerrors.Wrap(err, "tlimiter: redis command failed")
	}

	fields, ok := result.([]interface{})
	if !ok || len(fields) != 2 {
		return 0, 0, pkgerrors.New("tlimiter: expected Lua result with exactly two elements")
	}

	count, ok1 := fields[0].(int64)
	ttl, ok2 := fields[1].(int64)
	if !ok1 || !ok2 {
		return 0, 0, pkgerrors.New("tlimiter: expected count and ttl to be integer values")
	}

	return count, ttl, nil
}

// currentContext converts a Redis command result into a [Context] for the
// supplied rate.
func currentContext(cmd *redis.Cmd, rate Rate) (Context, error) {
	count, ttl, err := parseCountAndTTL(cmd)
	if err != nil {
		return Context{}, err
	}

	curTime := time.Now()

	expiration := curTime.Add(rate.Period)
	if ttl > 0 {
		expiration = curTime.Add(time.Duration(ttl) * time.Millisecond)
	}

	return GetContextFromState(rate, expiration, count), nil
}

// periodMilliseconds converts a duration to the millisecond value expected by
// Redis PEXPIRE.
//
// Durations between 0 and 1ms are rounded up so that they still expire.
func periodMilliseconds(period time.Duration) int64 {
	if period <= 0 {
		return 0
	}

	milliseconds := period / time.Millisecond
	if period%time.Millisecond != 0 {
		milliseconds++
	}

	return int64(milliseconds)
}
