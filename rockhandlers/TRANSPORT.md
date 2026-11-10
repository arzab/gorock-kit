# Writing your own App

You write your own App when there is no ready one for your technology. An App receives requests of its protocol (HTTP, a queue, gRPC, CLI, Telegram…) and hands them to rockhandlers routes. Reference implementation: [rockhttp](https://github.com/arzab/gorock-catalog/tree/main/rockhttp); a queue consumer example is below.

## Who does what

| rockhandlers (done) | The transport (yours) |
|---|---|
| the chain: middlewares → decoding → `Validate` → steps | receive a request / message of your protocol |
| panic recovery everywhere, including your decoding | fill `Params` from the request (the `fill` function) |
| error kinds, `Canceled` / `Timeout` from the context | turn the `Result` into a protocol response |
| Locals, middlewares, `Timeout` | turn the error **kind** into a protocol response |
| a new `Ctx` per call; the `Result` never leaks on error | the App lifecycle, shutdown, concurrency, limits |

Don't repeat in the transport what the chain already does: your own `recover` around `Serve`, validation, logging of route errors. Logging is a rockhandlers middleware.

## The minimal loop

```go
result, err := route.Serve(ctx, func(params any) error {
    return decode(req, params) // fill the new P from the request
})
if err != nil {
    writeError(resp, err)      // by rockhandlers.KindOf(err)
    return
}
writeResult(resp, result)      // result is the route's R (a pointer to a struct)
```

The rest of this guide is the details of these three lines.

## Step 1. Describe routes and check them at startup

Each transport has its own "mount point": HTTP has a method and a path, a queue has a queue name, a CLI has a command. Describe it with a struct in which the route is a plain field:

```go
type Consumer struct {
    Queue   string             // transport metadata
    Handler rockhandlers.Route // the chain
}
```

In the App's `Init`:

- **Validate the descriptions** (empty fields, duplicates) and return an error, so the engine won't start the App.
- **Add app-level middlewares:** `route := c.Handler.With(a.mws...)`. The first one is the outermost, and each `With` wraps everything added before. Do it once at registration, not per request.
- **Check the types** if the transport can't send everything: `route.ResultType()` and `route.ParamsType()` return the pointer `reflect.Type`s. For example, a CLI that prints text only can refuse a route returning `*rockhttp.File`, which is better at startup than on a request.
- **Prepare decoding ahead of time** if it is expensive: build the field/tag map once from `ParamsType()`.

A `Route` is immutable, so `Serve` may be called concurrently.

## Step 2. Decoding (`fill`)

`fill` gets a new `P` — a pointer to a zero struct — and fills it.

**Where it runs.** Inside the chain, after the middlewares and before `Validate`. So:
- middlewares (logging, metrics) see decoding errors — no separate reporting channel is needed;
- `Timeout` covers decoding;
- a panic in `fill` becomes `Internal` instead of crashing the process;
- if the context is already done, or a middleware doesn't call `Next`, `fill` does not run.

**Errors.**
- A plain `error` from `fill` becomes `Invalid`, **and the client sees its text.** Return a clear message without internals: `query "page": "x" is not an integer`.
- Return a `*rockhandlers.Error` to control the kind and the message: for example, `rockhandlers.Unauthorized("bad signature")` when verifying a webhook signature.
- For a body I/O failure the client can't act on, wrap it in a `*rockhandlers.Error` with `KindInternal`, so its text is hidden.

**Tags.** Reuse rockhttp's names where the meaning matches, so one `Params` model works across transports:

| Tag | HTTP | Queue | CLI |
|---|---|---|---|
| `json:"x"` | JSON body | JSON message body | JSON from stdin / a flag |
| `header:"X"` | header | message header (AMQP / Kafka headers) | env var |
| `query:"x"`, `path:"x"`, `form:"x"` | URL, path, form | — | flags / args |

Support **embedded structs**: reusable steps (auth, tenant) get their data through an embedded model like `BearerAuth` with `header:"Authorization"`.

**Limits.** Bound the body size in decoding itself (`http.MaxBytesReader` and the like); steps already receive a filled struct.

## Step 3. The result

On success, `result` is the route's `R` — a pointer to the filled struct.

- **By default** serialise it in your protocol's format (JSON, protobuf…).
- **Special result types** express what only your protocol can do: `*rockhttp.File` (stream a file), `*rockhttp.Redirect`. Declare them in the transport package and handle them with a type switch before serialising. A step that returns one is honestly tied to the transport, and that is fine.
- **An empty struct** (`struct{}`) is the convention for "nothing to send": HTTP answers 204, a queue just acknowledges.
- **Success status and other response metadata** that belong to the mount point go in its description (`Endpoint.Status`), not in the route.
- If writing the response fails after its start reached the client, nothing can be fixed; report it through a transport hook (`OnError` in rockhttp).

## Step 4. Errors

Translate the kind, not the error itself — `rockhandlers.KindOf(err)`:

| Kind | HTTP | Queue | CLI |
|---|---|---|---|
| `KindNone` (success) | 2xx | ack | 0 |
| `Invalid` | 400 | reject → DLQ | 2 |
| `Unauthorized` / `Forbidden` | 401 / 403 | reject → DLQ | 1 |
| `NotFound` / `Conflict` | 404 / 409 | reject → DLQ | 1 |
| `Unavailable` | 503 | requeue | 1 |
| `Timeout` | 504 | requeue or DLQ | 1 |
| `Canceled` | 499, no body | requeue | 130 |
| `Internal` | 500 | requeue with a retry limit | 1 |

Rules:

- **Only `rockhandlers.MessageOf(err)` goes to the client.** It returns "" for `Internal`: show a generic text ("internal error"). Never send `err.Error()` to the client — it carries causes meant for logs.
- **`rockhandlers.FieldsOf(err)`** returns per-field problems; send them if the protocol allows (JSON in HTTP, details in gRPC).
- **`Canceled` is not a server error:** the client is gone or the app is stopping. Don't write a body; in a queue, requeue the message so it isn't lost.
- **Unknown kinds** (from future versions) are handled as `Internal`.
- Logging route errors is a middleware's job, not the transport's.

## Step 5. Escape hatches through the context

When a step must affect the response in a way the result can't express (a cookie, a response header, message priority), or read the raw request, provide functions that work through the context:

```go
type exchangeKey struct{}

type exchange struct {
    req     *http.Request
    cookies []*http.Cookie
}

// Transport: before Serve.
ctx = context.WithValue(ctx, exchangeKey{}, &exchange{req: r})

// Public API of the transport: a step calls rockhttp.SetCookie(c, cookie).
func SetCookie(ctx context.Context, c *http.Cookie) {
    if x, ok := ctx.Value(exchangeKey{}).(*exchange); ok {
        x.cookies = append(x.cookies, c)
    }
}
```

A step's `Ctx` is a `context.Context`, so such functions take it directly. Outside a transport call they do nothing (or return nil), so steps stay testable with `rockhandlers.Run`. Apply what was collected when writing the response — both success and error.

## Step 6. Context, concurrency, shutdown

- **The request context** comes from the protocol (`r.Context()` in HTTP, cancelled when the connection drops) or from the App's `Exec` context for a queue. Pass it to `Serve`: cancellation reaches the steps and the database.
- **Cancellation is cooperative.** rockhandlers doesn't abandon steps in goroutines. It stops the chain between steps, and inside a step everything stops by itself if the step passes the context to blocking calls.
- **Concurrency is the transport's job:** worker pools, prefetch, a limit on in-flight requests. A single `Ctx` is not safe for concurrent use, but every `Serve` call gets its own.
- **Shutdown.** When the `Exec` context is cancelled, stop accepting new work and wait for in-flight messages. What happens to the ones that don't finish is up to the protocol: an HTTP server waits within its timeout, a queue returns unacknowledged messages. The App contract is in [rockengine](../rockengine/README.md).

## Step 7. Testing a transport

No broker is needed: build stub routes and check the translation.

```go
func TestKindMapping(t *testing.T) {
    type P struct{ ID string `json:"id"` }
    type R struct{}
    fail := func(err error) rockhandlers.Route {
        return rockhandlers.Chain("t", func(*rockhandlers.Ctx[*P, *R]) error { return err })
    }
    cases := []struct {
        route rockhandlers.Route
        want  outcome
    }{
        {fail(nil), ack},
        {fail(rockhandlers.Invalid("bad")), reject},
        {fail(rockhandlers.Unavailable("down")), requeue},
        {fail(errors.New("boom")), requeue},
        {rockhandlers.Chain("p", func(*rockhandlers.Ctx[*P, *R]) error { panic("x") }), requeue},
    }
    for _, c := range cases {
        if got := handle(c.route, message{Body: `{"id":"1"}`}); got != c.want {
            t.Fatalf("got %v, want %v", got, c.want)
        }
    }
}
```

Also test:
- a decoding error (`Invalid` with a clear text);
- that the `Internal` text is hidden;
- `Canceled` on an already-cancelled context;
- embedded structs in `Params`;
- special result types;
- that app middlewares see decoding errors too.

## Example: a queue consumer

```go
func (a *App) handle(ctx context.Context, msg *queue.Message) {
    route := a.routes[msg.Queue] // prepared in Init: Handler.With(a.mws...)
    _, err := route.Serve(ctx, func(params any) error {
        if err := json.Unmarshal(msg.Body, params); err != nil {
            return fmt.Errorf("invalid JSON body: %v", err) // → Invalid
        }
        return bindHeaders(msg.Headers, params) // header tags, embedded structs included
    })
    switch rockhandlers.KindOf(err) {
    case rockhandlers.KindNone:
        msg.Ack()
    case rockhandlers.KindUnavailable, rockhandlers.KindTimeout, rockhandlers.KindCanceled, rockhandlers.KindInternal:
        msg.Requeue()
    default:
        msg.Reject() // a bad message: retrying won't help
    }
}
```

This App's `Init`, `Exec` and `Stop` are on the [How to write an App](https://arzab.github.io/gorock/guide/apps) page of the GOROCK docs.

## Checklist

- [ ] A mount-point description with your metadata and a `Handler rockhandlers.Route` field.
- [ ] `Init`: validate descriptions, `With(mws...)` once, check `ResultType()` if needed.
- [ ] Decoding only inside `fill`: clear error texts, `*Error` for your own kinds, embedded structs, size limits.
- [ ] Result: the default format, special types, the empty struct.
- [ ] Errors by `KindOf`: only `MessageOf` to the client, a generic text for `Internal`, `FieldsOf`, and `Canceled` is not a server error.
- [ ] Escape hatches through the context, with no effect outside the transport.
- [ ] The request context goes to `Serve`; concurrency and shutdown are on the App's side.
- [ ] Tests for kind mapping, decoding errors, panics and cancellation.
