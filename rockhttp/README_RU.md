# rockhttp

> **Экспериментальный модуль.** Живёт в gorock-kit, чтобы проверить контракт `rockdelivery` на реальном транспорте; может переехать в gorock-catalog. API может меняться.

HTTP-транспорт для [rockdelivery](../rockdelivery) на `net/http` (шаблоны Go 1.22+), без зависимостей.

```go
server := rockhttp.New(cfg.HTTP,
    rockhttp.POST("/shops/{shop}/orders", orders.Create, rockhttp.Status(http.StatusCreated)),
    rockhttp.GET("/orders/{id}", orders.Get),
    rockhttp.GET("/files/{id}", files.Download), // возвращает *rockhttp.File
)
engine.MustRegister("http", server)
```

Хендлеры — обычные `rockdelivery.Handler[In, Out]`, про HTTP они ничего не знают.

## Вход

Теги: `path:"shop"`, `query:"dry_run"` (слайс — повтор параметра), `header:"X-Token"`, `form:"title"` (urlencoded / multipart), `form:"file"` с `*multipart.FileHeader` или `[]*multipart.FileHeader` для файлов; JSON-тело — по `json`-тегам. Порядок: JSON, form, query, header, path (последний побеждает). Ошибки конвертации и тело больше `MaxBodyBytes` → 400. `Validate()` вызывается после биндинга.

## Выход

- `*T` → JSON, статус из `Status(...)` или 200; `nil` → 204;
- `*rockhttp.File` → поток файла (Range и `If-Modified-Since`, если `Content` — `io.ReadSeeker`; `Download: true` → attachment);
- `*rockhttp.Redirect` → редирект (по умолчанию 302);
- ошибка → `ErrorBody{code, status, kind, message}`, сообщения Internal скрыты. Invalid 400, Unauthorized 401, Forbidden 403, NotFound 404, Conflict 409, Unavailable 503, Internal 500 (`StatusOf`).

То, что не выражается типизированным результатом, — через контекст: `rockhttp.SetCookie(ctx, c)`, `rockhttp.SetHeader(ctx, k, v)`, `rockhttp.Request(ctx)` (сырой запрос, запасной выход). Вне HTTP-вызова они ничего не делают / возвращают nil, поэтому хендлер остаётся тестируемым.

## Конфиг

`port` (обязателен), `prefix`, `shutdown_timeout` (30s), `read_timeout` (15s), `write_timeout` (0 = без лимита), `max_body_bytes` (10 MiB), `internal_message`. В коде: `OnFailure`, `Observe`, `Middleware` (уровень net/http: CORS, сжатие…). `App.Handler()` после `Init` отдаёт роутер для `httptest`.
