# rockhttp

> **Experimental.** Lives in gorock-kit to validate the `rockdelivery` contract on a real transport; may move to gorock-catalog. The API may change.

HTTP transport for [rockdelivery](../rockdelivery) on `net/http` (Go 1.22+ patterns), no dependencies.

```go
server := rockhttp.New(cfg.HTTP,
    rockhttp.POST("/shops/{shop}/orders", orders.Create, rockhttp.Status(http.StatusCreated)),
    rockhttp.GET("/orders/{id}", orders.Get),
    rockhttp.GET("/files/{id}", files.Download), // returns *rockhttp.File
)
engine.MustRegister("http", server)
```

Handlers are plain `rockdelivery.Handler[In, Out]` — they know nothing about HTTP.

## Input

```go
type CreateIn struct {
    ShopID int                   `path:"shop"`
    DryRun bool                  `query:"dry_run"`
    Tags   []string              `query:"tag"`   // ?tag=a&tag=b
    Token  string                `header:"X-Token"`
    Item   string                `json:"item"`   // JSON body
    Title  string                `form:"title"`  // urlencoded / multipart field
    File   *multipart.FileHeader `form:"file"`   // upload
}
```

Order: JSON body, then form, query, header, path (later wins). Conversion errors and bodies over `MaxBodyBytes` → 400. `Validate()` runs after binding.

## Output

| Handler returns | Response |
|---|---|
| `*T` | JSON, status from `Status(...)` or 200 |
| `nil` | 204 (or the `Status(...)` code) |
| `*rockhttp.File` | file stream; ranges and `If-Modified-Since` when `Content` is an `io.ReadSeeker`; `Download: true` → attachment |
| `*rockhttp.Redirect` | redirect, 302 by default |
| error | `ErrorBody{code, status, kind, message}`; Internal messages hidden |

Kinds → status: Invalid 400, Unauthorized 401, Forbidden 403, NotFound 404, Conflict 409, Unavailable 503, Internal 500 (`StatusOf`).

Things a typed result can't express go through the context: `rockhttp.SetCookie(ctx, c)`, `rockhttp.SetHeader(ctx, k, v)`, `rockhttp.Request(ctx)` (raw request, escape hatch). Outside an HTTP call they are no-ops / nil, so the handler stays testable.

## Config

```yaml
port: "8080"            # required
prefix: /api/v1
shutdown_timeout: 30s
read_timeout: 15s
write_timeout: 0s       # 0 = none (long downloads)
max_body_bytes: 10485760
internal_message: internal error
```

In code: `OnFailure`, `Observe` (logging / metrics), `Middleware` (net/http level: CORS, compression, ...). `App.Handler()` after `Init` gives the router for `httptest`.
