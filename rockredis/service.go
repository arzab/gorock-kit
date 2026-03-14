package rockredis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type service struct {
	cfg    Configs
	client *redis.Client
}

// NewService creates a Service that is ready to be initialised.
// No connection is opened — call Init to establish the client and verify connectivity.
func NewService(configs Configs) Service {
	return &service{cfg: configs}
}

// Init creates the Redis client and verifies connectivity via Ping.
func (s *service) Init(ctx context.Context) error {
	addr := s.cfg.Addr
	if addr == "" {
		addr = "localhost:6379"
	}

	opts := &redis.Options{
		Addr:         addr,
		Password:     s.cfg.Password,
		DB:           s.cfg.DB,
		DialTimeout:  dialOrDefault(s.cfg.DialTimeout, 5*time.Second),
		ReadTimeout:  dialOrDefault(s.cfg.ReadTimeout, 3*time.Second),
		WriteTimeout: dialOrDefault(s.cfg.WriteTimeout, 3*time.Second),
		PoolSize:     intOrDefault(s.cfg.PoolSize, 10),
		MinIdleConns: s.cfg.MinIdleConns,
	}
	if s.cfg.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	s.client = redis.NewClient(opts)
	return s.Ping(ctx)
}

// Stop releases the connection pool.
func (s *service) Stop() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func dialOrDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func intOrDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// wrapErr converts redis.Nil to ErrNil; other errors pass through unchanged.
func wrapErr(err error) error {
	if errors.Is(err, redis.Nil) {
		return ErrNil
	}
	return err
}

// toRedisZ converts our Z slice to go-redis Z slice.
func toRedisZ(members []Z) []redis.Z {
	out := make([]redis.Z, len(members))
	for i, m := range members {
		out[i] = redis.Z{Score: m.Score, Member: m.Member}
	}
	return out
}

// --- Core ---

func (s *service) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// --- String operations ---

func (s *service) Get(ctx context.Context, key string) (string, error) {
	val, err := s.client.Get(ctx, key).Result()
	return val, wrapErr(err)
}

func (s *service) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *service) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	ok, err := s.client.SetNX(ctx, key, value, ttl).Result()
	return ok, wrapErr(err)
}

func (s *service) Del(ctx context.Context, keys ...string) error {
	return s.client.Del(ctx, keys...).Err()
}

func (s *service) Exists(ctx context.Context, keys ...string) (int64, error) {
	n, err := s.client.Exists(ctx, keys...).Result()
	return n, wrapErr(err)
}

func (s *service) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ok, err := s.client.Expire(ctx, key, ttl).Result()
	return ok, wrapErr(err)
}

func (s *service) TTL(ctx context.Context, key string) (time.Duration, error) {
	d, err := s.client.TTL(ctx, key).Result()
	return d, wrapErr(err)
}

func (s *service) Incr(ctx context.Context, key string) (int64, error) {
	n, err := s.client.Incr(ctx, key).Result()
	return n, wrapErr(err)
}

func (s *service) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	n, err := s.client.IncrBy(ctx, key, value).Result()
	return n, wrapErr(err)
}

// --- Hash operations ---

func (s *service) HSet(ctx context.Context, key string, values ...any) error {
	if len(values) == 0 {
		return fmt.Errorf("rockredis: HSet requires at least one field/value pair")
	}
	return s.client.HSet(ctx, key, values...).Err()
}

func (s *service) HGet(ctx context.Context, key, field string) (string, error) {
	val, err := s.client.HGet(ctx, key, field).Result()
	return val, wrapErr(err)
}

func (s *service) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	m, err := s.client.HGetAll(ctx, key).Result()
	return m, wrapErr(err)
}

func (s *service) HDel(ctx context.Context, key string, fields ...string) error {
	return s.client.HDel(ctx, key, fields...).Err()
}

func (s *service) HExists(ctx context.Context, key, field string) (bool, error) {
	ok, err := s.client.HExists(ctx, key, field).Result()
	return ok, wrapErr(err)
}

// --- List operations ---

func (s *service) LPush(ctx context.Context, key string, values ...any) error {
	return s.client.LPush(ctx, key, values...).Err()
}

func (s *service) RPush(ctx context.Context, key string, values ...any) error {
	return s.client.RPush(ctx, key, values...).Err()
}

func (s *service) LPop(ctx context.Context, key string) (string, error) {
	val, err := s.client.LPop(ctx, key).Result()
	return val, wrapErr(err)
}

func (s *service) RPop(ctx context.Context, key string) (string, error) {
	val, err := s.client.RPop(ctx, key).Result()
	return val, wrapErr(err)
}

func (s *service) LLen(ctx context.Context, key string) (int64, error) {
	n, err := s.client.LLen(ctx, key).Result()
	return n, wrapErr(err)
}

func (s *service) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	vals, err := s.client.LRange(ctx, key, start, stop).Result()
	return vals, wrapErr(err)
}

// --- Set operations ---

func (s *service) SAdd(ctx context.Context, key string, members ...any) error {
	return s.client.SAdd(ctx, key, members...).Err()
}

func (s *service) SRem(ctx context.Context, key string, members ...any) error {
	return s.client.SRem(ctx, key, members...).Err()
}

func (s *service) SMembers(ctx context.Context, key string) ([]string, error) {
	vals, err := s.client.SMembers(ctx, key).Result()
	return vals, wrapErr(err)
}

func (s *service) SIsMember(ctx context.Context, key string, member any) (bool, error) {
	ok, err := s.client.SIsMember(ctx, key, member).Result()
	return ok, wrapErr(err)
}

func (s *service) SCard(ctx context.Context, key string) (int64, error) {
	n, err := s.client.SCard(ctx, key).Result()
	return n, wrapErr(err)
}

// --- Sorted set operations ---

func (s *service) ZAdd(ctx context.Context, key string, members ...Z) error {
	return s.client.ZAdd(ctx, key, toRedisZ(members)...).Err()
}

func (s *service) ZRem(ctx context.Context, key string, members ...any) error {
	return s.client.ZRem(ctx, key, members...).Err()
}

func (s *service) ZScore(ctx context.Context, key, member string) (float64, error) {
	score, err := s.client.ZScore(ctx, key, member).Result()
	return score, wrapErr(err)
}

func (s *service) ZRank(ctx context.Context, key, member string) (int64, error) {
	rank, err := s.client.ZRank(ctx, key, member).Result()
	return rank, wrapErr(err)
}

func (s *service) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	vals, err := s.client.ZRange(ctx, key, start, stop).Result()
	return vals, wrapErr(err)
}

func (s *service) ZCard(ctx context.Context, key string) (int64, error) {
	n, err := s.client.ZCard(ctx, key).Result()
	return n, wrapErr(err)
}

// --- Pub/Sub ---

func (s *service) Publish(ctx context.Context, channel string, message any) (int64, error) {
	n, err := s.client.Publish(ctx, channel, message).Result()
	return n, wrapErr(err)
}

func (s *service) Subscribe(ctx context.Context, channels ...string) *Subscription {
	pubSub := s.client.Subscribe(ctx, channels...)
	msgCh := make(chan *Message)

	go func() {
		defer close(msgCh)
		for msg := range pubSub.Channel() {
			msgCh <- &Message{Channel: msg.Channel, Payload: msg.Payload}
		}
	}()

	return &Subscription{
		ch:    msgCh,
		close: func() { _ = pubSub.Close() },
	}
}
