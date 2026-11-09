# rockhandlers — дизайн слоя (черновик на ревью)

Статус: **ядро реализовано** (см. README.md / README_RU.md). Отличие от черновика: `Ctx` не встраивает `context.Context`, а реализует его и даёт `Base()` / `SetContext()`. Ниже — исходный дизайн и оставшийся план.

## 1. Назначение

`rockhandlers` — форма обработки запроса, не зависящая от технологии. Это то, что во Fiber давали `Ctx`, `Next`, `Locals`, ErrorHandler и recover, только одинаково для HTTP, RabbitMQ, CLI и т.д.

Слой **не** бизнес-логика (она в Realm, `internal/`) и **не** доставка (её делают транспорты: `rockhttp`, будущий RabbitMQ-транспорт).

```
транспорт (rockhttp, rabbit…)        rockhandlers                       Realm
заполняет Params          ──▶  middleware → шаг 1 → шаг 2 … ──▶  сервисы/сценарии
интерпретирует Result/err ◀──  Result или ошибка с видом
```

### Граница ответственности

| Слой `rockhandlers` | Транспорт |
|---|---|
| цепочка шагов и `Next()` | заполнить `Params` из всего входящего: тело, заголовки, path, query, AMQP headers, флаги… |
| `Locals` между шагами | решить, что делать с `Result`: JSON, файл, редирект, ack, stdout… |
| перехват паники → ошибка | перевести вид ошибки в протокол: 404, nack в DLQ, exit code… |
| виды ошибок, отмена и таймаут | стриминг, повторы, prefetch, лимиты, протокольные middleware (CORS, gzip) |
| гарантия: всегда либо `Result`, либо ошибка | |

Правило для спорных случаев: **«имеет ли это смысл на другом транспорте?»** Нет — это транспорту.

**Приведений типов в пользовательском коде нет.** Всё, что работает с данными запроса, типизировано и проверяется компилятором.

## 2. Ctx и шаги

```go
// Ctx — состояние одного запроса. Сам является context.Context.
// P и R — типы-указатели на структуры: *CreateOrderParams, *CreateOrderResult.
type Ctx[P, R any] struct {
    context.Context          // отмена/дедлайн; передавать в БД, клиенты: db.Query(c, …)
    Params P                 // заполнил транспорт; не nil
    Result R                 // создан заранее; шаги его заполняют; не nil
}
func (c *Ctx[P, R]) Next() error   // следующий шаг
func (c *Ctx[P, R]) Route() string // имя маршрута (логи, метрики)

// Handler — один шаг цепочки.
type Handler[P, R any] func(c *Ctx[P, R]) error
```

`P` и `R` — сами указатели, а не `*P`. Поэтому ограничения-интерфейсы работают с методами на указателе (см. §3). `Chain` проверяет при сборке, что это указатели на структуры, иначе паника на старте.

Правила цепочки:
- шаг, который **не вызвал** `Next()`, завершает цепочку;
- ранний успешный выход разрешён: например, кэш заполнил `Result` и вернул `nil`;
- `Next()` после последнего шага — no-op, возвращает `nil`;
- повторный `Next()` — no-op, возвращает результат первого вызова;
- если `Params` реализует `Validator{ Validate() error }`, валидация — неявный первый шаг (после middleware). Простая ошибка из `Validate` → `Invalid`.

```go
var CreateOrder = rockhandlers.Chain("orders.create", auth.Bearer, orders.Create)
```

`Chain[P, R](name, steps...) Route` собирает цепочку и **стирает дженерики**: `Route` — обычный тип, маршруты разных типов лежат в одном слайсе / `Endpoint`. Типы выводятся из конкретного обработчика (`orders.Create`), а дженерик-шаги вроде `auth.Bearer` инстанцируются под них (вывод типов Go 1.21+).

## 3. Переиспользуемые шаги: ограничения вместо приведений

Шаг, которому нужно что-то из запроса, **объявляет требование интерфейсом**, а компилятор проверяет, что модель его выполняет.

```go
// internal/auth (или каталог: rockauth)
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

Модели выполняют требования **встраиванием**:

```go
// каталог/шаг поставляет и модель
type BearerAuth struct {
    Authorization string `header:"Authorization"` // откуда брать — решает транспорт по тегу
}
func (a *BearerAuth) BearerToken() string { return strings.TrimPrefix(a.Authorization, "Bearer ") }

// models
type CreateOrderParams struct {
    auth.BearerAuth          // встроили → *CreateOrderParams реализует auth.Params
    Item string `json:"item"`
    Qty  int    `json:"qty"`
}
```

- Не встроили `BearerAuth`, а `auth.Bearer` в цепочке — **ошибка компиляции**.
- Интерфейс объявляется **там, где используется** (в пакете шага). `models` только реализуют его — модели не зависят от шагов.
- То же для результата: `cache.Store[P any, R cache.Entry]` требует нужный интерфейс от `R`.
- **Каталог** поставляет готовые пары «шаг + встраиваемая модель»: `auth.Bearer` + `auth.BearerAuth`, `tenant.FromHeader` + `tenant.Header`, `paging.Limit` + `paging.Page`…

## 4. Middleware (не смотрят в данные)

Для сквозной логики, которой не нужны `Params`/`Result` — логирование, recover, timeout, метрики, трейсинг — есть нетипизированный middleware:

```go
type Context interface {
    context.Context
    Route() string
    Next() error
}
type Middleware func(c Context) error
```

`*Ctx[P, R]` реализует `Context`. Доступа к `Params()`/`Result()` как `any` **нет**: если middleware нужны данные — это типизированный шаг (§3).

**Обработчик ошибок, как ErrorHandler во Fiber**, — обычный middleware:

```go
func Log(c rockhandlers.Context) error {
    err := c.Next()
    if err != nil { /* лог, метрика, переклассификация */ }
    return err
}
```

- `route.With(mws...) Route` — копия маршрута с дополнительными внешними middleware. Так и группы, и транспорт (`app.Use(...)`) добавляют сквозную логику.
- Порядок выполнения: **middleware транспорта → middleware из `With` (внешние → внутренние) → валидация → шаги**.

## 5. Группы — обычные функции

В Go у методов не бывает своих параметров типа, поэтому группа — это **дженерик-функция в коде приложения**, а не объект библиотеки:

```go
// internal/routes/groups.go
func API[P, R any](name string, steps ...rockhandlers.Handler[P, R]) rockhandlers.Route {
    return rockhandlers.Chain(name, steps...).With(Log, rockhandlers.Timeout(5*time.Second))
}

func Admin[P interface{ auth.Params; tenant.Params }, R any](name string, steps ...rockhandlers.Handler[P, R]) rockhandlers.Route {
    return API(name, rockhandlers.Prepend(steps, auth.Bearer[P, R], admin.RequireRole[P, R])...)
}
```

```go
var BanUser = routes.Admin("users.ban", users.Ban)
```

- **Проверка при компиляции:** если модель не выполняет требования группы, код не соберётся.
- **Вложенность — обычный вызов функции,** порядок виден в коде.
- **От библиотеки нужны только `Chain`, `With` и хелпер `Prepend`.**
- **Позже, если шаблонный код начнёт мешать:** объект-группа `api.Group(auth.Bearer)` с проверкой типов через reflect при сборке маршрута — паника на старте, а не ошибка в рантайме. Добавляется без поломок.

## 6. Locals

```go
var UserKey = rockhandlers.NewLocal[*User]("user")

UserKey.Set(c, u)        // c — любой Context (шаг или middleware)
u, ok := UserKey.Get(c)
u := UserKey.MustGet(c)  // паника → Internal, если не положили (ошибка сборки цепочки)
```

- Типизированные ключи, без кастов и строковых коллизий. Дженерик — сам ключ `Local[T]`, поэтому `Get`/`MustGet` возвращают `T` без параметров типа при вызове (`UserKey.Get(c)` → `*User`).
- Хранятся в `Ctx`, живут один запрос.
- `Params` и `Result` — не в Locals, а полями `Ctx`: типизированно и всегда.

## 7. Ошибки

```go
type Error struct {
    Kind    Kind
    Message string            // для клиента
    Fields  map[string]string // ошибки по полям (валидация); может быть nil
    Err     error             // причина, только для логов
}
```

| Kind | Когда | HTTP (пример) | RabbitMQ (пример) |
|---|---|---|---|
| `Invalid` | плохой вход | 400 | nack → DLQ |
| `Unauthorized` | нет/плохие учётные данные | 401 | nack → DLQ |
| `Forbidden` | нет прав | 403 | nack → DLQ |
| `NotFound` | нет сущности | 404 | nack → DLQ |
| `Conflict` | конфликт состояния | 409 | nack → DLQ |
| `Unavailable` | временно недоступно | 503 | requeue |
| `Canceled` | ctx отменён (клиент ушёл / остановка) | 499 без тела | requeue |
| `Timeout` | дедлайн ctx | 504 | requeue / DLQ |
| `Internal` | всё прочее, паника | 500, текст скрыт | requeue с лимитом |

- Конструкторы: `Invalid(msg, args...)`, `NotFound(...)`, … и `Invalid(...).WithFields(map)`.
- `KindOf(err)`: `*Error` в цепочке → его вид; `context.Canceled` → `Canceled`; `context.DeadlineExceeded` → `Timeout`; иначе `Internal`.
- `MessageOf(err)` для `Internal` возвращает `""` — транспорт показывает свой общий текст.
- Паника → `Internal`, причина — `*PanicError{Value, Stack}`, чтобы было что логировать.
- Колонки HTTP и RabbitMQ — решение транспортов, не слоя.

## 8. Отмена и таймауты

Используется нативная модель Go — кооперативная, как во Fiber и `net/http`:

1. `Ctx` — это `context.Context`. Шаги передают `c` во все блокирующие вызовы (БД, HTTP, каналы). Эти вызовы сами мгновенно возвращаются по сигналу.
2. `Next()` не запускает следующий шаг, если `c.Err() != nil`, и возвращает классифицированную ошибку.
3. `Timeout(d) Middleware` — дедлайн на ctx оставшейся цепочки.
4. Логика «при отмене» — стандартная библиотека:
   - `defer` — уборка;
   - `context.AfterFunc(c, fn)` — реакция сразу по сигналу;
   - `context.WithoutCancel(c)` — работа, которая должна завершиться вопреки отмене.

Слой **не** запускает шаги в отдельных горутинах и не бросает их.

## 9. Контракт для транспорта

```go
type Route struct{ /* поля скрыты */ }

func (r Route) Name() string
func (r Route) NewParams() any                 // новый P (указатель на нулевую структуру) — заполнить
func (r Route) ParamsType() reflect.Type       // для проверок при регистрации
func (r Route) ResultType() reflect.Type       // транспорт может отказать неподдерживаемому R при старте
func (r Route) With(mws ...Middleware) Route
func (r Route) Serve(ctx context.Context, params any) (result any, err error)
```

Как работает транспорт:

```go
params := route.NewParams()
if err := decode(req, params); err != nil { /* сам: обычно Invalid */ }
result, err := route.Serve(ctx, params)
if err != nil { /* KindOf(err) → протокол */ } else { /* result → протокол */ }
```

Единственное место с `any` — граница с транспортом. Внутри библиотеки это безопасно: `Serve` проверяет, что `params` того самого типа, иначе `Internal`.

Гарантии `Serve`:
- паника не выходит наружу;
- при ошибке `result == nil` — частично заполненный `Result` не утекает;
- при успехе `result` имеет тип `R`;
- новый `Ctx` на каждый вызов, без пула — `c` безопасно захватывать.

Что `R` означает «нет тела» — например `*struct{}` или пустая структура — решает транспорт.

## 10. Тестирование без транспорта

```go
res, err := rockhandlers.Run[*CreateOrderParams, *CreateOrderResult](ctx, CreateOrder, &CreateOrderParams{…})
```

Прогоняет всю цепочку с middleware и валидацией. Если `P`/`R` не совпадают с маршрутом, возвращает понятную ошибку. Отдельные шаги тестируются ещё проще — это обычные функции от `*Ctx`.

## 11. Вне слоя (явно)

- **Стриминг** (SSE, WebSocket, отдача по частям) — запасной выход транспорта.
- **Повторы, DLQ, prefetch, лимиты параллельности, backpressure** — транспорт / App.
- **Протокольные middleware** (CORS, gzip, rate limit по IP) — транспорт.
- **Готовые шаги** (auth, tenant, paging, кэш, логгер) — каталог или приложение. Слой даёт только механизм.

---

# План работ

1. ✅ **Переименование.** `rockdelivery` → `rockhandlers` (rockdelivery удалён): каталог, `go.mod`, `go.work`, матрица CI, `replace` в `rockhttp`.
2. **Ядро `rockhandlers`**:
   - файлы:
     - `ctx.go` — Ctx, Context, Next;
     - `chain.go` — Chain, Route, With, Serve, Prepend;
     - `locals.go`;
     - `errors.go` — + Canceled, Timeout, Fields, PanicError;
     - `timeout.go`;
     - `run.go`;
   - удалить Pipeline / Codec / MetaKey / Action;
   - тесты:
     - порядок middleware и шагов, ранний выход, повторный `Next`;
     - отмена между шагами, паника в шаге и в middleware;
     - `Result` не утекает при ошибке;
     - дженерик-шаг с ограничением и встроенной моделью, «группа-функция»;
     - `Locals`, `Run` с неверными типами, `Chain` с не-указателем;
     - `-race`.
3. **`rockhttp` на новый слой**:
   - `Endpoint{Method, Path, Handler rockhandlers.Route, Status int}`, `App.Use(...)` через `With`;
   - биндинг тегами, включая встроенные структуры (`BearerAuth` с `header:"…"`);
   - `File` / `Redirect` как `R`;
   - ошибки: `Fields` в JSON, `Canceled` → 499 без тела, `Timeout` → 504;
   - проверка `ResultType` при `Init`.
4. **Доказательство независимости от транспорта.** В тестах — игрушечный in-memory транспорт («очередь»): одна и та же цепочка, шаги и middleware работают и в `rockhttp`, и в нём. Настоящий RabbitMQ-транспорт и готовые шаги (`auth.Bearer`…) — позже, в gorock-catalog.
5. **Документация.**
   - README / README_RU для `rockhandlers` (из этого файла) и `rockhttp`;
   - `example_test.go`;
   - корневой README: «как слои сочетаются»;
   - CLAUDE.md (Architecture);
   - CHANGELOG: Unreleased, rockdelivery → rockhandlers.
6. **Проверка:** `check.sh all`, `tidy-check`, `-race` и lint в Docker. Коммит и push — по команде.
7. **Позже:** документация gorock, плагин и AGENTS.md под новое ядро; релиз v0.3.0; RabbitMQ-транспорт и готовые шаги в каталоге.

## Решения

1. Название — **`rockhandlers`**.
2. `Canceled` в HTTP — **499** (для логов прокси; тело не пишется, клиента уже нет).
3. `MustGet` — **оставляем**.

## Открытые вопросы

1. **Статус успеха в HTTP.** Предложение: статический `Endpoint.Status` (201 для создания и т.п.), по умолчанию 200. Динамический статус от результата (`R` с методом `HTTPStatus() int`) — позже и только если понадобится: он затаскивает HTTP в модели.
