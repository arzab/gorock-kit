# rockhttp

> **Экспериментальный модуль.** Живёт в gorock-kit, чтобы проверить [rockhandlers](../rockhandlers) на реальном транспорте; может переехать в gorock-catalog. API может меняться.

HTTP-транспорт для rockhandlers на `net/http` (шаблоны Go 1.22+), без зависимостей.

```go
server := rockhttp.New(cfg.HTTP,
    rockhttp.Endpoint{Method: "POST", Path: "/shops/{shop}/orders", Handler: orders.Create, Status: http.StatusCreated},
    rockhttp.Endpoint{Method: "GET", Path: "/orders/{id}", Handler: orders.Get},
    rockhttp.Endpoint{Method: "GET", Path: "/files/{id}", Handler: files.Download}, // R = *rockhttp.File
).Use(Log) // middleware rockhandlers вокруг всех эндпоинтов
engine.MustRegister("http", server)
```

`Handler` — это `rockhandlers.Route` (`rockhandlers.Chain(...)`): шаги ничего не знают про HTTP.

## Params

Поля заполняются по тегам:
- `path:"shop"` — параметр пути;
- `query:"dry_run"` — параметр запроса; для слайса параметр повторяют;
- `header:"…"` — заголовок;
- `form:"title"` — поле формы, urlencoded или multipart;
- `form:"file"` с `*multipart.FileHeader` или `[]*multipart.FileHeader` — загрузка файлов;
- JSON-тело — по `json`-тегам.

Теги во встроенных структурах тоже биндятся. Например, `auth.BearerAuth` с `header:"Authorization"`.

Порядок: JSON, form, query, header, path — следующий источник перезаписывает предыдущий. Ошибки конвертации и тело больше `MaxBodyBytes` → 400.

## Result

| Тип результата `R` | Ответ |
|---|---|
| структура с полями | JSON; статус из `Endpoint.Status`, по умолчанию 200 |
| пустая структура | 204 (или `Endpoint.Status`) |
| `*rockhttp.File` | поток файла; Range и `If-Modified-Since`, если `Content` — `io.ReadSeeker`; `Download: true` → attachment |
| `*rockhttp.Redirect` | редирект, по умолчанию 302 |
| ошибка | `ErrorBody{code, status, kind, message, fields}`; сообщения Internal скрыты |

Вид ошибки → HTTP-статус:

| Kind | Статус |
|---|---|
| Invalid | 400 |
| Unauthorized | 401 |
| Forbidden | 403 |
| NotFound | 404 |
| Conflict | 409 |
| Unavailable | 503 |
| Timeout | 504 |
| Canceled | 499, без тела |
| Internal | 500 |

Это соответствие отдаёт функция `StatusOf`.

То, что не выражается типизированным результатом, — через контекст:
- `rockhttp.SetCookie(c, cookie)`;
- `rockhttp.SetHeader(c, k, v)`;
- `rockhttp.Request(c)` — сырой запрос, запасной выход.

Вне HTTP-вызова они ничего не делают или возвращают nil, поэтому шаги остаются тестируемыми.

## Конфиг

```yaml
port: "8080"            # обязателен
prefix: /api/v1
shutdown_timeout: 30s
read_timeout: 15s
write_timeout: 0s       # 0 = без лимита (долгие скачивания)
max_body_bytes: 10485760
internal_message: internal error
```

В коде:
- `OnError(r, route, err)` — вызывается, только если запись ответа сломалась после успешной цепочки. Ошибки запросов, включая ошибки разбора, логируйте в middleware rockhandlers через `Use`: биндинг выполняется внутри цепочки.
- `Middleware` — уровень net/http (CORS, gzip…).

После `Init` метод `App.Handler()` отдаёт роутер для `httptest`.
