# rockhandlers

The handlers layer of a GOROCK project: business logic as a chain of handlers — like Fiber, but not tied to HTTP. The same chain is run by an HTTP server, a queue consumer or cron. Standard library only.

## Handler

Everything a handler needs comes as arguments, like in `net/http`:

```go
type Handler[P Params, R Response] func(c *Ctx, params P, response R) error
```

- `Params` — what came in: body, headers, path. The App fills it. It must have `Validate() error`.
- `Response` — what goes back: handlers fill it. It must have `Normalize() error`.
- `Ctx` — the request context (`context.Context`) with `Next()` and `Set`/`Get` values.

```go
// internal/handlers/tasks/create.go
type CreateParams struct {
    Token string `header:"Authorization"`
    Title string `json:"title"`
}

func (p *CreateParams) Validate() error {
    if strings.TrimSpace(p.Title) == "" {
        return rockhandlers.Invalid("invalid task").WithFields(map[string]string{"title": "required"})
    }
    return nil
}

type CreateResponse struct {
    Task *models.Task `json:"task"`
}

func (r *CreateResponse) Normalize() error { return nil }

func checkAuth(c *rockhandlers.Ctx, params *CreateParams, _ *CreateResponse) error {
    if params.Token == "" {
        return rockhandlers.Unauthorized("token required")
    }
    return c.Next() // go on
}

func create(c *rockhandlers.Ctx, params *CreateParams, response *CreateResponse) error {
    task := &models.Task{ID: uuid.NewString(), Title: params.Title}
    if err := services.Repository().Insert(c, task); err != nil {
        return err
    }
    response.Task = task
    return nil // done
}

var Create = rockhandlers.Chain(checkAuth, create)
```

`Chain` infers the params and response types from the handlers. A handler with other types does not compile.

## The chain — like Fiber

- `return nil` — the chain is done, without errors.
- `return c.Next()` — run the next handler and return its result.
- `return err` — the chain stops, the App gets the error.
- `c.Next()` with no handlers left returns `ErrHandlerNotFound`, so the last handler returns `nil`.

A chain may be empty — a skeleton. Running it returns `ErrHandlerNotFound`, and the HTTP server answers `501 Not Implemented`.

## Run order

The App calls `pipeline.Run(ctx, fill)`:

1. **middlewares;**
2. **`fill`** — the App decodes the request into the params;
3. **`params.Validate()`;**
4. **handlers;**
5. **`response.Normalize()`** — only if there was no error.

| What went wrong | What the App gets |
|---|---|
| a plain error from `fill` | Invalid with the message `invalid request`; the details stay in the cause, for logs |
| a plain error from `Validate` | Invalid with its text |
| `rockhandlers.Invalid(...)` and other kinds | as is |
| a panic anywhere | Internal with `*PanicError` |
| an error from `Normalize` | `*NormalizeError` (tell it apart with `errors.As`) |

## Middlewares

A middleware is the same handler, with the general interfaces instead of concrete structs, since it runs in front of any chain:

```go
type Middleware = Handler[Params, Response]

func Log(c *rockhandlers.Ctx, _ rockhandlers.Params, _ rockhandlers.Response) error {
    err := c.Next()
    if err != nil {
        rocklog.Error("request failed", rocklog.Err(err))
    }
    return err
}

Create.Use(Log, rockhandlers.Timeout(5*time.Second))
```

`Use` puts middlewares in front of the chain, and each `Use` goes in front of the previous ones, so an App adds its middlewares at registration and they run first.

## Request values

```go
c.Set(userKey{}, user)    // in one handler
v, ok := c.Get(userKey{}) // in the next
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
- **`MessageOf`** hides Internal messages; **`FieldsOf`** returns the per-field problems.

## Cancellation

`Ctx` is a `context.Context`: pass it to the database, clients and channels. `Next` does not start the next handler once the request is cancelled. `Timeout(d)` is a middleware with a deadline.

## Testing

```go
res, err := tasks.Create.Call(ctx, &tasks.CreateParams{Token: "t", Title: "write docs"})
```

`Call` does everything `Run` does, with ready params: `Validate`, handlers, `Normalize`.

## For Apps

```go
response, err := pipeline.Run(ctx, func(params rockhandlers.Params) error {
    return decode(req, params) // fill the empty params from the request
})
// err → reply by KindOf(err); response → the protocol's reply
```

Details: [Writing your own App](TRANSPORT.md).
