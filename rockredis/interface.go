package rockredis

import (
	"context"
	"errors"
	"time"
)

// ErrNil is returned when a key does not exist in Redis.
var ErrNil = errors.New("rockredis: nil")

// Z represents a scored member used in sorted set operations.
type Z struct {
	Score  float64
	Member any
}

// Message is a pub/sub message received from a Redis channel.
type Message struct {
	Channel string
	Payload string
}

// Subscription wraps a Redis pub/sub subscription.
// Read incoming messages from Channel() and call Close() when done.
type Subscription struct {
	ch    <-chan *Message
	close func()
}

// Channel returns the read-only channel of incoming messages.
func (s *Subscription) Channel() <-chan *Message { return s.ch }

// Close unsubscribes and releases resources.
func (s *Subscription) Close() { s.close() }

// Service is the rockredis abstraction over a Redis client.
//
// Call order: Init → use → Stop.
type Service interface {
	// Init creates the Redis client and verifies connectivity via Ping.
	Init(ctx context.Context) error

	// Stop releases the connection pool.
	Stop() error

	// Ping checks the Redis connection.
	Ping(ctx context.Context) error

	// --- String operations ---

	// Get returns the value of key. Returns ErrNil if the key does not exist.
	Get(ctx context.Context, key string) (string, error)

	// Set stores value under key with an optional TTL (0 = no expiry).
	Set(ctx context.Context, key string, value any, ttl time.Duration) error

	// SetNX sets value only if key does not exist. Returns true if the key was set.
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)

	// Del removes one or more keys.
	Del(ctx context.Context, keys ...string) error

	// Exists returns the number of the provided keys that exist.
	Exists(ctx context.Context, keys ...string) (int64, error)

	// Expire sets a TTL on key. Returns false if the key does not exist.
	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)

	// TTL returns the remaining time-to-live of key.
	// Returns -1 if the key has no TTL, -2 if it does not exist.
	TTL(ctx context.Context, key string) (time.Duration, error)

	// Incr increments the integer value of key by 1.
	Incr(ctx context.Context, key string) (int64, error)

	// IncrBy increments the integer value of key by value.
	IncrBy(ctx context.Context, key string, value int64) (int64, error)

	// --- Hash operations ---

	// HSet sets fields in a hash. values must be alternating field/value pairs
	// or a map[string]any / struct with redis tags.
	HSet(ctx context.Context, key string, values ...any) error

	// HGet returns the value of a hash field. Returns ErrNil if missing.
	HGet(ctx context.Context, key, field string) (string, error)

	// HGetAll returns all fields and values of a hash.
	HGetAll(ctx context.Context, key string) (map[string]string, error)

	// HDel removes fields from a hash.
	HDel(ctx context.Context, key string, fields ...string) error

	// HExists reports whether a hash field exists.
	HExists(ctx context.Context, key, field string) (bool, error)

	// --- List operations ---

	// LPush prepends values to a list (right-most argument is first element).
	LPush(ctx context.Context, key string, values ...any) error

	// RPush appends values to a list.
	RPush(ctx context.Context, key string, values ...any) error

	// LPop removes and returns the first element of a list. Returns ErrNil if empty.
	LPop(ctx context.Context, key string) (string, error)

	// RPop removes and returns the last element of a list. Returns ErrNil if empty.
	RPop(ctx context.Context, key string) (string, error)

	// LLen returns the length of a list.
	LLen(ctx context.Context, key string) (int64, error)

	// LRange returns a slice of list elements between start and stop (inclusive).
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)

	// --- Set operations ---

	// SAdd adds members to a set.
	SAdd(ctx context.Context, key string, members ...any) error

	// SRem removes members from a set.
	SRem(ctx context.Context, key string, members ...any) error

	// SMembers returns all members of a set.
	SMembers(ctx context.Context, key string) ([]string, error)

	// SIsMember reports whether member is in the set.
	SIsMember(ctx context.Context, key string, member any) (bool, error)

	// SCard returns the number of members in a set.
	SCard(ctx context.Context, key string) (int64, error)

	// --- Sorted set operations ---

	// ZAdd adds or updates scored members in a sorted set.
	ZAdd(ctx context.Context, key string, members ...Z) error

	// ZRem removes members from a sorted set.
	ZRem(ctx context.Context, key string, members ...any) error

	// ZScore returns the score of member. Returns ErrNil if member is absent.
	ZScore(ctx context.Context, key, member string) (float64, error)

	// ZRank returns the rank (0-based, ascending) of member. Returns ErrNil if absent.
	ZRank(ctx context.Context, key, member string) (int64, error)

	// ZRange returns members between start and stop ranks (ascending, inclusive).
	ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)

	// ZCard returns the number of members in a sorted set.
	ZCard(ctx context.Context, key string) (int64, error)

	// --- Pub/Sub ---

	// Publish sends message to channel. Returns the number of subscribers that received it.
	Publish(ctx context.Context, channel string, message any) (int64, error)

	// Subscribe subscribes to channels and returns a Subscription.
	// Call Subscription.Close() when done to release resources.
	Subscribe(ctx context.Context, channels ...string) *Subscription
}
