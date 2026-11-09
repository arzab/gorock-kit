# rockdelivery

The contract between Realm (`internal/delivery`) and the Apps that serve it. Business logic is a typed function that knows nothing about the transport; any App — HTTP, queue consumer, scheduler, CLI — can serve the same function. Standard library only; no transport here.

## Handler

```go
// internal/delivery/orders/create.go
type CreateParams struct {
    Item string `json:"item"`
    Qty  int    `json:"qty"`
}

func (p *CreateParams) Validate() error {
    if p.Qty <= 0 {
        return rockdelivery.Invalid("qty must be > 0")
    }
    return nil
}

type CreateResult struct {
    Order *models.Order `json:"order"`
}

func Create(ctx context.Context, p *CreateParams) (*CreateResult, error) {
    order := &models.Order{ID: uuid.NewString(), Item: p.Item, Qty: p.Qty}
    if err := repository.Get().Insert(ctx, order); err != nil {
        return nil, fmt.Errorf("insert order: %w", err) // Internal
    }
    return &CreateResult{Order: order}, nil
}
```

`func(ctx, *In) (*Out, error)`. In and Out are structs; no input or output → `struct{}`. Split long logic into ordinary functions; cross-cutting concerns go into middlewares.

## Errors

Kinds say *what went wrong*, not *which status to send* — each App maps them to its protocol.

| Constructor | Kind | Meaning |
|---|---|---|
| `Invalid(msg, args...)` | `KindInvalid` | bad input |
| `Unauthorized(...)` | `KindUnauthorized` | caller not authenticated |
| `Forbidden(...)` | `KindForbidden` | caller may not do this |
| `NotFound(...)` | `KindNotFound` | the thing does not exist |
| `Conflict(...)` | `KindConflict` | duplicate, version mismatch |
| `Unavailable(...)` | `KindUnavailable` | temporary; retrying may help |
| any other error | `KindInternal` | unexpected; message hidden from clients |

```go
return rockdelivery.NotFound("order %q not found", id)
return rockdelivery.Unavailable("payments are down").Wrap(err) // cause kept for logs

rockdelivery.KindOf(err)    // kind of the first *Error in the chain, else KindInternal
rockdelivery.MessageOf(err) // client-safe message; "" for Internal
```

`Validate` may return a plain error — it becomes `Invalid` with that message.

## Serving a handler (for App authors)

```go
action := rockdelivery.Bind(orders.Create, rockdelivery.Timeout(5*time.Second))

in := action.NewInput()              // *CreateParams — decode the request into it
out, err := action.Call(ctx, in)     // middlewares → Validate → handler; panics become Internal errors
// map out to the response, rockdelivery.KindOf(err) to the protocol
```

`InputType()` / `OutputType()` describe the handler for generated docs.

## Pipeline: writing a transport

Every request goes through the same steps whatever the technology. Only three of them depend on it, and that is all a transport implements:

```
request ─► [Decode] ─► middlewares ─► Validate ─► handler ─┬─► [Encode]       ─► response
              │                                             └─► [EncodeError]  ─► error response
              └─ decode error → Invalid
```

```go
type Codec[Req, Resp any] interface {
    Decode(ctx context.Context, r Route, req Req, in any) error           // fill *In from the request
    Encode(ctx context.Context, r Route, resp Resp, out any) error        // write the result
    EncodeError(ctx context.Context, r Route, resp Resp, f Failure) error // write the failure
}
```

`Req` is the technology's request (`*http.Request`, a queue message, CLI args); `Resp` is where the answer goes (`http.ResponseWriter`, an acker, an exit code).

```go
p := rockdelivery.NewPipeline[*http.Request, http.ResponseWriter](httpCodec{},
    rockdelivery.InternalMessage("internal error"),                               // client text for Internal
    rockdelivery.OnFailure(func(ctx context.Context, f rockdelivery.Failure) {}), // logs: full error
    rockdelivery.Observe(func(ctx context.Context, r rockdelivery.Route, err error, took time.Duration) {}), // metrics
)

err := p.Serve(ctx, route, req, resp) // per request; the answer is written either way
```

`Serve` does the common part:

- a decode error becomes `Invalid` (an `*Error` from `Decode` keeps its kind);
- a failure becomes a `Failure{Route, Stage, Kind, Message, Err}`; `Message` is safe for the client — Internal errors get the configured text, the real error stays in `Err`;
- panics anywhere — handler, middleware, the `Codec` itself, the hooks — become Internal failures; the App never crashes;
- if writing the result fails, the failure is reported (`StageEncode`) instead of writing an error over a half-written answer;
- it returns the request's error, so an App can act on it (a consumer deciding to retry).

### Route metadata

A transport's per-route options travel with the route as typed metadata. The transport declares the keys; business code never reads them.

```go
// in the transport package
var Status = rockdelivery.NewMetaKey[int]("http.status")

// where routes are declared
route := rockdelivery.NewRoute("orders.create", rockdelivery.Bind(orders.Create),
    rockdelivery.WithMeta(Status, 201))

// in the Codec
status := rockdelivery.MetaOr(r, Status, 200)
```

Keys are compared by identity, so two transports can't clash even with the same name.

## Middleware

```go
type Middleware func(next Next) Next // Next = func(ctx, in any) (any, error)
```

Transport-independent: auth from ctx, logging, metrics. The first middleware passed to `Bind` is the outermost; a middleware may return an error without calling `next`. Built in: `Timeout(d)`.
