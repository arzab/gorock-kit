# rockdelivery

Контракт между Realm (`internal/delivery`) и Apps, которые его обслуживают. Бизнес-логика — типизированная функция, которая ничего не знает о транспорте; любой App — HTTP, consumer очереди, планировщик, CLI — может обслуживать одну и ту же функцию. Только стандартная библиотека; транспорта здесь нет.

## Обработчик

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

`func(ctx, *In) (*Out, error)`. In и Out — структуры; нет входа или выхода — `struct{}`. Длинную логику разбивайте на обычные функции; общие вещи — в middleware.

## Ошибки

Вид говорит, *что пошло не так*, а не *какой статус отправить* — каждый App переводит его в свой протокол.

| Конструктор | Вид | Смысл |
|---|---|---|
| `Invalid(msg, args...)` | `KindInvalid` | неверный вход |
| `Unauthorized(...)` | `KindUnauthorized` | вызывающий не аутентифицирован |
| `Forbidden(...)` | `KindForbidden` | вызывающему нельзя |
| `NotFound(...)` | `KindNotFound` | объекта нет |
| `Conflict(...)` | `KindConflict` | дубликат, конфликт версий |
| `Unavailable(...)` | `KindUnavailable` | временно; повтор может помочь |
| любая другая ошибка | `KindInternal` | непредвиденная; текст скрывается от клиента |

```go
return rockdelivery.NotFound("order %q not found", id)
return rockdelivery.Unavailable("payments are down").Wrap(err) // причина сохраняется для логов

rockdelivery.KindOf(err)    // вид первой *Error в цепочке, иначе KindInternal
rockdelivery.MessageOf(err) // безопасное для клиента сообщение; "" для Internal
```

`Validate` может вернуть обычную ошибку — она станет `Invalid` с этим сообщением.

## Обслуживание обработчика (для авторов App)

```go
action := rockdelivery.Bind(orders.Create, rockdelivery.Timeout(5*time.Second))

in := action.NewInput()              // *CreateParams — декодировать в него запрос
out, err := action.Call(ctx, in)     // middleware → Validate → обработчик; паники становятся Internal-ошибками
// out — в ответ, rockdelivery.KindOf(err) — в протокол
```

`InputType()` / `OutputType()` описывают обработчик, например для генерации документации.

## Pipeline: как написать транспорт

Каждый запрос проходит одни и те же шаги независимо от технологии. От неё зависят только три из них — это всё, что реализует транспорт:

```
запрос ─► [Decode] ─► middleware ─► Validate ─► обработчик ─┬─► [Encode]       ─► ответ
             │                                               └─► [EncodeError]  ─► ответ-ошибка
             └─ ошибка разбора → Invalid
```

```go
type Codec[Req, Resp any] interface {
    Decode(ctx context.Context, r Route, req Req, in any) error           // заполнить *In из запроса
    Encode(ctx context.Context, r Route, resp Resp, out any) error        // записать результат
    EncodeError(ctx context.Context, r Route, resp Resp, f Failure) error // записать ошибку
}
```

`Req` — запрос технологии (`*http.Request`, сообщение очереди, аргументы CLI); `Resp` — куда отдаётся ответ (`http.ResponseWriter`, acker, код выхода).

```go
p := rockdelivery.NewPipeline[*http.Request, http.ResponseWriter](httpCodec{},
    rockdelivery.InternalMessage("internal error"),                               // текст для клиента при Internal
    rockdelivery.OnFailure(func(ctx context.Context, f rockdelivery.Failure) {}), // логи: полная ошибка
    rockdelivery.Observe(func(ctx context.Context, r rockdelivery.Route, err error, took time.Duration) {}), // метрики
)

err := p.Serve(ctx, route, req, resp) // на каждый запрос; ответ записан в любом случае
```

`Serve` делает общую часть:

- ошибка разбора становится `Invalid` (`*Error` из `Decode` сохраняет свой вид);
- сбой превращается в `Failure{Route, Stage, Kind, Message, Err}`; `Message` безопасен для клиента — для Internal подставляется настроенный текст, настоящая ошибка остаётся в `Err`;
- паники где угодно — в обработчике, middleware, самом `Codec`, хуках — становятся Internal-сбоями; App не падает;
- если не удалось записать результат, сбой сообщается (`StageEncode`), а не пишется ошибка поверх наполовину отправленного ответа;
- возвращает ошибку запроса, чтобы App мог на неё отреагировать (consumer решает, повторять ли).

### Метаданные маршрута

Настройки транспорта для конкретного маршрута передаются вместе с маршрутом как типизированные метаданные. Ключи объявляет транспорт; бизнес-код их не читает.

```go
// в пакете транспорта
var Status = rockdelivery.NewMetaKey[int]("http.status")

// там, где объявляются маршруты
route := rockdelivery.NewRoute("orders.create", rockdelivery.Bind(orders.Create),
    rockdelivery.WithMeta(Status, 201))

// в Codec
status := rockdelivery.MetaOr(r, Status, 200)
```

Ключи сравниваются по идентичности, поэтому два транспорта не конфликтуют даже при одинаковых именах.

## Middleware

```go
type Middleware func(next Next) Next // Next = func(ctx, in any) (any, error)
```

Не зависят от транспорта: авторизация из ctx, логирование, метрики. Первый middleware, переданный в `Bind`, — внешний; middleware может вернуть ошибку, не вызывая `next`. Встроенный: `Timeout(d)`.
