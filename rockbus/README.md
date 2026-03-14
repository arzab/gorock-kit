# rockbus

An in-process event bus for Go applications. Allows layers within an application to communicate via typed events without direct dependencies between them.

Each topic gets its own dedicated goroutine and buffered queue, which guarantees that events within the same topic are processed **in order**. Different topics are processed **concurrently** and do not block each other.

Satisfies the rockengine `App` interface (`Init / Exec / Stop`).

## Quick Start

```go
// Define subscriptions in the delivery layer
var Subscriptions = []rockbus.Subscription{
    rockbus.On("user.created",  onUserCreated),
    rockbus.On("order.placed",  onOrderPlaced),
}

// Create App with subscriptions
app := rockbus.NewApp(rockbus.Config{
    QueueSize: 1024,
    OnError: func(ctx context.Context, event rockbus.Event, err error) {
        log.Error("bus error", rocklog.Str("topic", string(event.Topic)), rocklog.Err(err))
    },
}, Subscriptions...)

// Set as global default
rockbus.SetDefault(app)

// Register with rockengine
engine.MustRegister("bus", app, rockengine.RestartPolicy{})
engine.Run()
```

## Config

```go
type Config struct {
    // Buffer capacity of each per-topic async queue. Default: 1024.
    // PublishAsync drops events when the queue is full and calls OnError.
    QueueSize int

    // Called when an async handler errors, panics, queue is full,
    // app is stopped, or topic has no worker. Nil = silently dropped.
    OnError func(ctx context.Context, event Event, err error)
}
```

## Lifecycle

```
NewApp(cfg, subs...) → SetDefault → Init → Exec (blocks) → Stop
```

- **NewApp** accepts subscriptions directly — all topics are known before startup.
- **Subscribe** can be called additionally, but **before Exec** — otherwise no worker is created.
- **Init** allocates the queue channels and resets state. Safe to call again after `Stop` for restart scenarios.
- **Exec** blocks until `ctx` is cancelled or `Stop` is called. Workers drain their queues before exiting.
- **Stop** is idempotent and safe to call before `Init`.

## Subscribing

### Via constructor (recommended)

```go
type UserCreated struct {
    UserID int
    Email  string
}

func onUserCreated(ctx context.Context) error {
    p, err := rockbus.Payload[UserCreated](ctx)
    if err != nil {
        return err
    }
    fmt.Println("user created:", p.UserID)
    return nil
}

var Subscriptions = []rockbus.Subscription{
    rockbus.On("user.created", onUserCreated),
    rockbus.On("order.placed", onOrderPlaced),
}

app := rockbus.NewApp(cfg, Subscriptions...)
```

### Via Subscribe (before Exec)

```go
app.Subscribe("user.created", func(ctx context.Context) error {
    p, err := rockbus.Payload[UserCreated](ctx)
    if err != nil {
        return err
    }
    return nil
})

// Or via the package-level function
rockbus.Subscribe("user.created", onUserCreated)
```

Multiple handlers can be registered for the same topic — all are called in registration order.

## Payload in Context

Each handler receives a `ctx` with the payload and topic name injected:

```go
func onUserCreated(ctx context.Context) error {
    // Type-safe payload retrieval
    p, err := rockbus.Payload[UserCreated](ctx)
    if err != nil {
        return err
    }

    // Current topic (if needed)
    topic := rockbus.CurrentTopic(ctx) // "user.created"

    fmt.Println("user created:", p.UserID, p.Email)
    return nil
}
```

`Payload[T]` returns `ErrPayload` if the payload is missing or the type does not match.

## Publishing

### Synchronous

Runs handlers in the **caller's goroutine**, in registration order. Blocks until all handlers complete. All handlers are called even if one fails.

```go
err := rockbus.Publish(ctx, rockbus.Event{
    Topic:   "user.created",
    Payload: UserCreated{UserID: 1, Email: "alice@example.com"},
})
// err is a combined error from all failed handlers (errors.Join)
```

Use `Publish` when:
- The result matters to the caller
- You need guaranteed ordering with the next operation
- The operation is part of a transaction

### Asynchronous

Puts the event into the topic's queue and **returns immediately**. The dedicated worker goroutine picks it up and processes it.

```go
rockbus.PublishAsync(ctx, rockbus.Event{
    Topic:   "user.created",
    Payload: UserCreated{UserID: 1, Email: "alice@example.com"},
})
```

Use `PublishAsync` for side effects that should not block the caller: sending emails, updating caches, analytics, notifications.

The caller's context cancellation and deadline are **detached** — the worker runs to completion regardless of whether the HTTP request that triggered the event has already ended.

## Ordering Guarantee

Within a single topic, events are always processed **in the order they were published**:

```
PublishAsync "user.updated" {name: "Alice"} ──► worker processes first
PublishAsync "user.updated" {name: "Bob"}   ──► worker processes second (guaranteed)
```

Different topics are independent and run concurrently:

```
"user.updated"  ──► Worker A  (ordered)
"order.placed"  ──► Worker B  (ordered, independent of A)
```

## Context Values

Pass metadata through events using context helpers:

```go
// Before publishing
ctx = rockbus.WithValue(ctx, "traceId", "abc-123")
ctx = rockbus.WithValue(ctx, "userID", 42)

rockbus.PublishAsync(ctx, event)

// In the handler
func onUserCreated(ctx context.Context) error {
    traceId, _ := rockbus.GetValue[string](ctx, "traceId")
    userID, err := rockbus.GetValue[int](ctx, "userID")
    // ...
}
```

`GetValue[T]` returns an error if the key is missing or the type does not match.

## Error Handling

### Sentinel errors

```go
errors.Is(err, rockbus.ErrQueueFull)  // topic queue is full, event dropped
errors.Is(err, rockbus.ErrAppStopped) // app stopped, event dropped
errors.Is(err, rockbus.ErrNoWorker)   // topic has no worker (Subscribe after Exec)
errors.Is(err, rockbus.ErrPanic)      // handler panicked, panic recovered
errors.Is(err, rockbus.ErrPayload)    // payload missing or wrong type
```

### OnError hook

```go
rockbus.NewApp(rockbus.Config{
    OnError: func(ctx context.Context, event rockbus.Event, err error) {
        switch {
        case errors.Is(err, rockbus.ErrQueueFull):
            metrics.Inc("bus.queue_full", "topic", string(event.Topic))
        case errors.Is(err, rockbus.ErrPanic):
            log.Error("handler panic", rocklog.Str("topic", string(event.Topic)), rocklog.Err(err))
        default:
            log.Warn("bus error", rocklog.Str("topic", string(event.Topic)), rocklog.Err(err))
        }
    },
})
```

`OnError` is called for async errors only. `Publish` (sync) returns errors directly to the caller.

A panic inside `OnError` itself is recovered and written to `stderr` — it will never bring down a worker goroutine.

## Panic Recovery

Both `Publish` and `PublishAsync` recover panics from handlers. A panic is converted to an `ErrPanic`-wrapped error:

- **Publish**: returned as part of the combined error
- **PublishAsync**: passed to `OnError`

The stack trace is included in the error message.

## Limitations

- **Subscribe after Exec**: topics registered after `Exec` starts have no worker. `PublishAsync` calls `OnError` with `ErrNoWorker`. `Publish` still works — it runs in the caller's goroutine.
- **One worker per topic**: if a single topic receives events faster than the handler can process them, the queue fills up. This is a signal to move that handler to a separate service. See `ErrQueueFull`.
- **No wildcard topics**: subscribing to `"user.*"` is not supported. Use explicit topic names.
- **No unsubscribe**: handlers cannot be removed after registration.
