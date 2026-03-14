# rockredis

A Redis client wrapper built on [go-redis/v9](https://github.com/redis/go-redis) that integrates with `rockconfig` and provides a clean, typed `Service` interface covering the most common Redis operations.

## Quick Start

```go
svc := rockredis.NewService(rockredis.Configs{
    Addr:     "localhost:6379",
    Password: "secret",
    DB:       0,
})

ctx := context.Background()
if err := svc.Ping(ctx); err != nil {
    log.Fatal(err)
}
defer svc.Close()
```

## Loading Config from File

`rockredis.Configs` is compatible with `rockconfig.InitFromFile`. All fields are optional.

```yaml
# config.yaml
addr: "localhost:6379"
password: "secret"
db: 0
dial_timeout: "5s"
read_timeout: "3s"
write_timeout: "3s"
pool_size: 10
min_idle_conns: 2
tls_enabled: false
```

```go
cfg, err := rockconfig.InitFromFile[rockredis.Configs]("config.yaml")
if err != nil {
    log.Fatal(err)
}
svc := rockredis.NewService(*cfg)
```

## Config

```go
type Configs struct {
    Addr     string        // host:port; default "localhost:6379"
    Password string
    DB       int           // Redis database index (0–15)

    DialTimeout  time.Duration // default 5s
    ReadTimeout  time.Duration // default 3s
    WriteTimeout time.Duration // default 3s

    PoolSize     int // connection pool size; default 10
    MinIdleConns int // minimum idle connections

    TLSEnabled bool // enable TLS (uses system cert pool)
}
```

## ErrNil

When a key or field does not exist, operations return `rockredis.ErrNil` (translated from `redis.Nil`):

```go
val, err := svc.Get(ctx, "missing-key")
if errors.Is(err, rockredis.ErrNil) {
    // key does not exist
}
```

## Operations

### Strings

```go
// Set with TTL (0 = no expiry)
svc.Set(ctx, "key", "value", 10*time.Minute)

// Get
val, err := svc.Get(ctx, "key")

// Set only if not exists
ok, err := svc.SetNX(ctx, "lock", "1", 30*time.Second)

// Delete
svc.Del(ctx, "key1", "key2")

// Check existence (returns count of found keys)
n, err := svc.Exists(ctx, "key1", "key2")

// TTL management
svc.Expire(ctx, "key", 5*time.Minute)
ttl, err := svc.TTL(ctx, "key") // -1 = no TTL, -2 = key missing

// Counters
svc.Incr(ctx, "counter")
svc.IncrBy(ctx, "counter", 10)
```

### Hash

```go
// Set fields (alternating field/value pairs)
svc.HSet(ctx, "user:1", "name", "Alice", "age", 30)

// Get one field
val, err := svc.HGet(ctx, "user:1", "name")

// Get all fields
m, err := svc.HGetAll(ctx, "user:1")

// Delete fields
svc.HDel(ctx, "user:1", "age")

// Check field existence
ok, err := svc.HExists(ctx, "user:1", "name")
```

### List

```go
// Push
svc.LPush(ctx, "queue", "first", "second") // "second" becomes head
svc.RPush(ctx, "queue", "last")

// Pop (returns ErrNil if empty)
val, err := svc.LPop(ctx, "queue")
val, err  = svc.RPop(ctx, "queue")

// Length
n, err := svc.LLen(ctx, "queue")

// Range (inclusive, 0-based; -1 = last element)
vals, err := svc.LRange(ctx, "queue", 0, -1)
```

### Set

```go
svc.SAdd(ctx, "tags", "go", "redis")
svc.SRem(ctx, "tags", "redis")
members, err := svc.SMembers(ctx, "tags")
ok, err      := svc.SIsMember(ctx, "tags", "go")
n, err       := svc.SCard(ctx, "tags")
```

### Sorted Set

```go
svc.ZAdd(ctx, "leaderboard",
    rockredis.Z{Score: 100, Member: "alice"},
    rockredis.Z{Score: 200, Member: "bob"},
)

score, err  := svc.ZScore(ctx, "leaderboard", "alice")
rank, err   := svc.ZRank(ctx, "leaderboard", "alice")  // 0-based, ascending
vals, err   := svc.ZRange(ctx, "leaderboard", 0, -1)    // ascending
n, err      := svc.ZCard(ctx, "leaderboard")

svc.ZRem(ctx, "leaderboard", "alice")
```

### Pub/Sub

```go
// Publisher
n, err := svc.Publish(ctx, "events", "hello")

// Subscriber
sub := svc.Subscribe(ctx, "events", "other-channel")
defer sub.Close()

for msg := range sub.Channel() {
    fmt.Println(msg.Channel, msg.Payload)
}
```

`Subscription.Close()` unsubscribes and closes the underlying connection. The channel returned by `Channel()` is closed automatically when `Close()` is called.

## TLS

```go
svc := rockredis.NewService(rockredis.Configs{
    Addr:       "redis.example.com:6380",
    TLSEnabled: true,
})
```

Uses the system certificate pool. For custom certificates, set `TLSEnabled: false` and connect via a custom setup outside `rockredis`.

## Limitations

- `Subscribe.Channel()` is a buffered goroutine bridge — slow consumers can delay message delivery.
- `TLSEnabled: true` uses the system cert pool; mutual TLS requires a custom `redis.Options` setup.
- `ZRank` uses ascending order. For descending rank use `redis.Client.ZRevRank` directly via `NewClient` and casting.
