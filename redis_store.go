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

// Client is the minimal Redis API surface required by [RedisStore] (standalone or cluster clients).
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

	// luaMutex protects script SHA fields during load and concurrent reads.
	luaMutex sync.RWMutex
	// luaLoaded is non-zero after both Lua scripts have been loaded successfully.
	luaLoaded uint32
	// luaIncrSHA is the SHA1 of the loaded increment script (EVALSHA).
	luaIncrSHA string
	// luaPeekSHA is the SHA1 of the loaded peek script (EVALSHA).
	luaPeekSHA string
}

// NewRedisStore returns a [Store] using [DefaultPrefix] and [DefaultCleanUpInterval].
func NewRedisStore(client Client) (Store, error) {
	return NewRedisStoreWithOptions(client, StoreOptions{
		Prefix:          DefaultPrefix,
		CleanUpInterval: DefaultCleanUpInterval,
	})
}

// NewRedisStoreWithOptions returns a [Store] for the given client and [StoreOptions].
func NewRedisStoreWithOptions(client Client, options StoreOptions) (Store, error) {
	if isNilClient(client) {
		return nil, errors.New("tlimiter: redis client is nil")
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

// isNilClient reports whether c is an unusable [Client] (including typed nil pointers).
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

// Increment implements [Store.Increment]. A negative count returns an error; see package documentation.
func (store *RedisStore) Increment(ctx context.Context, key string, count int64, rate Rate) (Context, error) {
	if count < 0 {
		return Context{}, errors.New("tlimiter: increment count must not be negative")
	}
	cmd := store.evalSHA(ctx, store.getLuaIncrSHA, []string{store.getCacheKey(key)}, count, rate.Period.Milliseconds())

	return currentContext(cmd, rate)
}

// Get implements [Store.Get].
func (store *RedisStore) Get(ctx context.Context, key string, rate Rate) (Context, error) {
	cmd := store.evalSHA(ctx, store.getLuaIncrSHA, []string{store.getCacheKey(key)}, 1, rate.Period.Milliseconds())

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

// getCacheKey builds the Redis key for the application-level key.
func (store *RedisStore) getCacheKey(key string) string {
	buffer := strings.Builder{}

	buffer.WriteString(store.Prefix)
	buffer.WriteString(":")
	buffer.WriteString(key)

	return buffer.String()
}

// preloadLuaScripts ensures Lua scripts are loaded at most once per process state.
func (store *RedisStore) preloadLuaScripts(ctx context.Context) error {
	// Verify if we need to load lua scripts.
	// Inspired by sync.Once.
	if atomic.LoadUint32(&store.luaLoaded) == 0 {
		return store.loadLuaScripts(ctx)
	}

	return nil
}

// reloadLuaScripts forces a reload of both Lua scripts (for example after NOSCRIPT).
func (store *RedisStore) reloadLuaScripts(ctx context.Context) error {
	// Reset lua scripts loaded state.
	// Inspired by sync.Once.
	atomic.StoreUint32(&store.luaLoaded, 0)

	return store.loadLuaScripts(ctx)
}

// loadLuaScripts loads both scripts via SCRIPT LOAD and stores their SHA digests.
// Use preloadLuaScripts or reloadLuaScripts; do not call loadLuaScripts directly.
func (store *RedisStore) loadLuaScripts(ctx context.Context) error {
	store.luaMutex.Lock()
	defer store.luaMutex.Unlock()

	// Check if scripts are already loaded.
	if atomic.LoadUint32(&store.luaLoaded) != 0 {
		return nil
	}

	luaIncrSHA, err := store.client.ScriptLoad(ctx, luaIncrScript).Result()
	if err != nil {
		return pkgerrors.Wrap(err, `failed to load "incr" lua script`)
	}

	luaPeekSHA, err := store.client.ScriptLoad(ctx, luaPeekScript).Result()
	if err != nil {
		return pkgerrors.Wrap(err, `failed to load "peek" lua script`)
	}

	store.luaIncrSHA = luaIncrSHA
	store.luaPeekSHA = luaPeekSHA

	atomic.StoreUint32(&store.luaLoaded, 1)

	return nil
}

// getLuaIncrSHA returns the increment script SHA (read-locked).
func (store *RedisStore) getLuaIncrSHA() string {
	store.luaMutex.RLock()
	defer store.luaMutex.RUnlock()

	return store.luaIncrSHA
}

// getLuaPeekSHA returns the peek script SHA (read-locked).
func (store *RedisStore) getLuaPeekSHA() string {
	store.luaMutex.RLock()
	defer store.luaMutex.RUnlock()

	return store.luaPeekSHA
}

// evalSHA executes EVALSHA for getSha(); on NOSCRIPT it reloads scripts and retries once.
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

// isLuaScriptGone reports whether err is a Redis NOSCRIPT error.
func isLuaScriptGone(err error) bool {
	return strings.HasPrefix(err.Error(), "NOSCRIPT")
}

// parseCountAndTTL parses the {count, ttl ms} pair returned by Lua scripts.
func parseCountAndTTL(cmd *redis.Cmd) (int64, int64, error) {
	result, err := cmd.Result()
	if err != nil {
		return 0, 0, pkgerrors.Wrap(err, "an error has occurred with redis command")
	}

	fields, ok := result.([]interface{})
	if !ok || len(fields) != 2 {
		return 0, 0, pkgerrors.New("two elements in result were expected")
	}

	count, ok1 := fields[0].(int64)
	ttl, ok2 := fields[1].(int64)
	if !ok1 || !ok2 {
		return 0, 0, pkgerrors.New("type of the count and/or ttl should be number")
	}

	return count, ttl, nil
}

// currentContext converts a Redis command result into a [Context] for rate.
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
