# Writing your own App

You write your own App when there is no ready one for your technology: a queue, gRPC, a bot, a CLI. An App receives requests of its protocol and runs handler chains. A ready example: [rockhttp](https://github.com/arzab/gorock-catalog/tree/main/rockhttp).

## Who does what

| rockhandlers (done) | The App (yours) |
|---|---|
| order: middlewares → decoding → `Validate` → handlers → `Normalize` | receive a request |
| panic recovery everywhere | decode the request into the params (the `fill` function) |
| error kinds, cancellation, timeouts | turn the response and the error kind into the protocol's reply |
| | start, stop, concurrency, limits |

Don't repeat in the App what is already there: your own `recover`, params validation, logging of handler errors (that is a middleware).

## The main loop

```go
response, err := pipeline.Run(ctx, func(params rockhandlers.Params) error {
    return decode(req, params) // params is a new, empty params struct
})
if err != nil {
    writeError(resp, err) // by rockhandlers.KindOf(err)
    return
}
writeResponse(resp, response)
```

## 1. Mounting chains

Each App has its own entry point: HTTP has a method and a path, a queue has a queue name, a CLI has a command. Describe it with a struct where the chain is a plain field:

```go
type Consumer struct {
    Queue   string
    Handler *rockhandlers.Pipeline
}
```

In the App's `Init`, validate the descriptions and add your middlewares: `pipeline.Use(a.middlewares...)` — once, at registration.

## 2. Decoding (`fill`)

`fill` gets a new, empty params struct and fills it.

- It runs inside the chain, after the middlewares, so middlewares see decoding errors and timeouts cover decoding.
- A plain error from `fill` becomes Invalid with the message `invalid request`; the details stay in the cause, for logs.
- To tell the client more, return `rockhandlers.Invalid("invalid JSON body")` or an error with fields — without Go type or struct names.
- Reuse rockhttp's tags where the meaning matches: `json` — body, `header` — headers (HTTP or message), `path`, `query`, `form`. Then one params struct works in several Apps.

## 3. The response

On success `response` is the filled response struct. Send it in your protocol's format by default. Special response types for your protocol (like `rockhttp.File` and `rockhttp.Redirect`) go into the App's package and are handled with a type switch.

## 4. Errors

Translate the kind, not the error itself:

| Kind | HTTP | Queue | CLI |
|---|---|---|---|
| `KindNone` (success) | 2xx | ack | 0 |
| `Invalid`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict` | 400 / 401 / 403 / 404 / 409 | to the dead-letter queue | 1–2 |
| `Unavailable`, `Timeout` | 503 / 504 | retry later | 1 |
| `Canceled` | 499, no body | requeue | 130 |
| `Internal` | 500 | retry with a limit | 1 |

- Only `rockhandlers.MessageOf(err)` goes to the client; it is empty for Internal — show a generic text.
- `rockhandlers.FieldsOf(err)` — per-field problems, if the protocol can carry them.
- `errors.Is(err, rockhandlers.ErrHandlerNotFound)` — the chain is not written yet: HTTP answers `501`.
- `*rockhandlers.NormalizeError` — the response failed `Normalize`: a server error.

## 5. Context and shutdown

- The request context comes from the protocol (`r.Context()` in HTTP) or from the App's `Exec`. Pass it to `Run`.
- Concurrency, limits and prefetch are the App's job.
- On shutdown, stop taking new work and wait for the work in progress.

## 6. Tests

No broker is needed: build stub chains and check the kind translation.

```go
type P struct{}
func (*P) Validate() error { return nil }
type R struct{}
func (*R) Normalize() error { return nil }

fail := func(err error) *rockhandlers.Pipeline {
    return rockhandlers.Chain(func(*rockhandlers.Ctx, *P, *R) error { return err })
}
// fail(nil) → ack, fail(rockhandlers.Invalid("bad")) → reject, fail(errors.New("boom")) → requeue
```
