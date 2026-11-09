# rockhttp

> **Experimental.** Lives in gorock-kit to validate [rockhandlers](../rockhandlers) on a real transport; may move to gorock-catalog. The API may change.

HTTP transport for rockhandlers on `net/http` (Go 1.22+ patterns), no dependencies.

```go
server := rockhttp.New(cfg.HTTP,
    rockhttp.Endpoint{Method: "POST", Path: "/shops/{shop}/orders", Handler: orders.Create, Status: http.StatusCreated},
    rockhttp.Endpoint{Method: "GET", Path: "/orders/{id}", Handler: orders.Get},
    rockhttp.Endpoint{Method: "GET", Path: "/files/{id}", Handler: files.Download}, // R = *rockhttp.File
).Use(Log) // rockhandlers middlewares around every endpoint
engine.MustRegister("http", server)
```

`Handler` is a `rockhandlers.Route` (`rockhandlers.Chain(...)`): the steps know nothing about HTTP.

## Params

```go
type CreateParams struct {
    auth.BearerAuth                    // embedded: its `header:"Authorization"` is bound too
    ShopID int                   `path:"shop"`
    DryRun bool                  `query:"dry_run"`
    Tags   []string              `query:"tag"`   // ?tag=a&tag=b
    Item   string                `json:"item"`   // JSON body
    Title  string                `form:"title"`  // urlencoded / multipart field
    File   *multipart.FileHeader `form:"file"`   // upload
}
```

Binding order: JSON body, then form, query, header, path; a later source wins. Tags inside embedded structs are bound too. Conversion errors and bodies over `MaxBodyBytes` → 400.

## Result

| Result type `R` | Response |
|---|---|
| struct with fields | JSON; status from `Endpoint.Status`, default 200 |
| empty struct | 204 (or `Endpoint.Status`) |
| `*rockhttp.File` | file stream; ranges and `If-Modified-Since` when `Content` is an `io.ReadSeeker`; `Download: true` → attachment |
| `*rockhttp.Redirect` | redirect, 302 by default |
| error | `ErrorBody{code, status, kind, message, fields}`; Internal messages are hidden |

Error kind → HTTP status:

| Kind | Status |
|---|---|
| Invalid | 400 |
| Unauthorized | 401 |
| Forbidden | 403 |
| NotFound | 404 |
| Conflict | 409 |
| Unavailable | 503 |
| Timeout | 504 |
| Canceled | 499, no body |
| Internal | 500 |

`StatusOf` exposes this mapping.

What a typed result can't express goes through the context:
- `rockhttp.SetCookie(c, cookie)`;
- `rockhttp.SetHeader(c, k, v)`;
- `rockhttp.Request(c)` — the raw request, as an escape hatch.

Outside an HTTP call these are no-ops (or return nil), so the steps stay testable.

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

In code:
- `OnError(r, route, err)` — called only when writing a response fails after a successful chain. Log request errors (including decoding errors) in a rockhandlers middleware added with `Use`: binding runs inside the chain.
- `Middleware` — net/http level (CORS, gzip…).

After `Init`, `App.Handler()` returns the router for `httptest`.
