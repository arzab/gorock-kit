# rockhandlers

A transport-independent shape for handling a request: what Fiber gives you with `Ctx`, `Next`, `Locals`, an error handler and recover, but the same for HTTP, queues, CLI and anything else. No dependencies.

```
transport (HTTP, queue…)          rockhandlers                         Realm
fills Params           ──▶  middlewares → step 1 → step 2 … ──▶  services / use cases
maps Result / error    ◀──  Result, or an error with a Kind
```

The layer owns the chain, `Next`, Locals, panic recovery, error kinds and cancellation. The transport owns the protocol: it fills `Params` (body, headers, path, AMQP headers, flags…), turns the `Result` into a response (JSON, file, redirect, ack, stdout…) and the error kind into its protocol (status, nack, exit code).

## Steps and chains

```go
type CreateParams struct {
    auth.BearerAuth          // embedded model → the auth.Bearer step accepts these params
    Item string `json:"item"`
    Qty  int    `json:"qty"`
}
type CreateResult struct{ ID string `json:"id"` }

func Create(c *rockhandlers.Ctx[*CreateParams, *CreateResult]) error {
    id, err := orders.Create(c, c.Params.Item, c.Params.Qty) // c is a context.Context
    if err != nil {
        return err
    }
    c.Result.ID = id
    return nil
}

var CreateOrder = rockhandlers.Chain("orders.create", auth.Bearer, Create)
```

Long step signatures can be shortened with a type alias next to the model — the same type, no runtime cost:

```go
type CreateCtx = rockhandlers.Ctx[*CreateParams, *CreateResult]

func Create(c *CreateCtx) error { … }
```

- `Ctx[P, R]` holds typed `Params`, filled by the transport, and `Result`, which is allocated before the chain and filled by the steps. P and R are pointer-to-struct types.
- A step that returns `nil` hands control to the next step — calling `c.Next()` is not required. Call it when you need to run code after the rest of the chain.
- To end the chain early with success (for example, a cache hit), return `rockhandlers.Done`: the remaining steps are skipped, outer middlewares see `nil`, and the Result is returned. A middleware that returns `Done` before the steps skips validation and the steps.
- A repeated `Next()` returns the first result. `Next()` after the last step returns nil.
- If `Params` implements `Validate() error`, it runs after the transport has filled `Params` and before the first step: check first, then use. A plain error from it becomes `Invalid`.
- `Chain` erases the generics. A `Route` is a plain value, so routes of different types fit in one slice.

## Reusable steps: constraints instead of casts

A step that needs request data declares it with an interface, and the compiler checks that the model satisfies it:

```go
type Params interface{ BearerToken() string }

func Bearer[P Params, R any](c *rockhandlers.Ctx[P, R]) error {
    user, err := verify(c, c.Params.BearerToken())
    if err != nil {
        return rockhandlers.Unauthorized("invalid token")
    }
    UserKey.Set(c, user)
    return c.Next()
}
```

Models satisfy it by embedding, like `auth.BearerAuth` above. If you forget to embed it, the code does not compile. `Chain` infers P and R from the concrete step and instantiates generic steps to match.

## Middlewares

Use middlewares for cross-cutting logic that does not look at request data: logging, metrics, tracing, timeouts, error handling.

```go
func Log(c rockhandlers.Context) error {
    err := c.Next()
    if err != nil { /* log, metric, re-classify */ }
    return err
}

route = route.With(Log, rockhandlers.Timeout(5*time.Second))
```

`With` wraps the route:
- The first middleware is the outermost, and each `With` call wraps everything added before it. A transport adds its app-level middlewares the same way, with `route.With(...)` at registration.
- Execution order: transport middlewares → `With` → decoding (the transport fill) → validation → steps.
- `c.SetContext(ctx)` replaces the context for the rest of the request.

## Groups

A group is a plain generic function in your code, because Go methods cannot have type parameters:

```go
func Admin[P auth.Params, R any](name string, steps ...rockhandlers.Handler[P, R]) rockhandlers.Route {
    return rockhandlers.Chain(name, rockhandlers.Prepend(steps, auth.Bearer[P, R], admin.Require[P, R])...).
        With(Log)
}

var BanUser = Admin("users.ban", users.Ban)
```

## Locals

```go
var UserKey = rockhandlers.NewLocal[*User]("user")

UserKey.Set(c, u)
u, ok := UserKey.Get(c)  // *User, no cast
u := UserKey.MustGet(c)  // panics (→ Internal) if the step that sets it is missing
```

## Errors

- **Constructors:** `Invalid`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `Unavailable`.
- **Details:** `.WithFields(map)` adds per-field problems; `.Wrap(err)` adds the cause, for logs only.
- **`KindOf(err)`:**
  - nil → `KindNone`;
  - an `*Error` → its kind;
  - `context.Canceled` → `KindCanceled`;
  - `context.DeadlineExceeded` → `KindTimeout`;
  - anything else → `KindInternal`.
- **`MessageOf`** returns the client message and hides Internal messages; **`FieldsOf`** returns the per-field problems.
- **Panics:** a panic anywhere in the chain becomes an Internal error with `*PanicError{Value, Stack}`. Middlewares see it as an ordinary error.

## Cancellation

Cancellation is native and cooperative. `Ctx` is a `context.Context`: pass it to the database, clients and channels, and they return as soon as it is cancelled. `Next` does not start further steps once the context is done.

For logic that runs on cancellation, use the standard library:
- `defer` for cleanup;
- `context.AfterFunc(c, fn)` to react at once;
- `context.WithoutCancel(c)` for work that must finish anyway.

## For transports

```go
result, err := route.Serve(ctx, func(params any) error {
    return decode(req, params) // fill the new P from the request
})                             // never panics; on error result is nil
// err → protocol by KindOf(err); result (an R) → protocol
```

The fill function runs inside the chain, after the middlewares and before validation, so middlewares see decoding errors and timeouts cover decoding. A plain error from fill becomes `Invalid` with its text; return an `*Error` to control the kind and the message.

`ParamsType()` and `ResultType()` let a transport check routes at startup, for example to refuse a result type it cannot send.

Step-by-step guide with a checklist: [Writing your own App](TRANSPORT.md).

## Testing

```go
res, err := rockhandlers.Run[*CreateResult](ctx, CreateOrder, &CreateParams{Qty: 1}) // P is inferred
```

`Run` executes the whole chain (middlewares, validation, steps) without a transport. Steps are plain functions and can also be tested alone.

## Out of scope

- **For transports:** streaming (SSE, WebSocket), retries, DLQ, prefetch, concurrency limits, protocol middlewares (CORS, gzip).
- **For your code or the catalog:** ready-made steps such as auth, tenant, paging, caching and logging.
