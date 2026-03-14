# rockengine

Движок для оркестрирования жизненного цикла приложений на Go.

## Обзор

`rockengine` управляет жизненным циклом нескольких именованных приложений: последовательная инициализация, параллельное исполнение, политики перезапуска и graceful shutdown.

**Жизненный цикл app:**

```
Init → Exec → Stop
```

- **Init** — вызывается последовательно в порядке регистрации; инициализирует ресурсы.
- **Exec** — выполняется параллельно в отдельной горутине; должен уважать отмену `ctx`.
- **Stop** — вызывается при остановке; должен разблокировать `Exec` и дождаться текущих операций.

## Реализация App

```go
type App interface {
    Init(ctx context.Context) error
    Exec(ctx context.Context) error
    Stop() []error
}
```

**Пример:**

```go
type WorkerApp struct {
    ticker *time.Ticker
    done   chan struct{}
}

func (a *WorkerApp) Init(_ context.Context) error {
    a.ticker = time.NewTicker(time.Second)
    a.done = make(chan struct{})
    return nil
}

func (a *WorkerApp) Exec(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-a.ticker.C:
            fmt.Println("tick")
        }
    }
}

func (a *WorkerApp) Stop() []error {
    a.ticker.Stop()
    close(a.done)
    return nil
}
```

## Быстрый старт

```go
package main

import (
    "log"
    "github.com/arzab/gorock-kit/rockengine"
)

func main() {
    rockengine.Register("worker", &WorkerApp{})
    rockengine.Register("http",   &HTTPApp{})

    if err := rockengine.Run(); err != nil {
        log.Fatal(err)
    }
}
```

`Run` блокируется до получения `SIGINT` / `SIGTERM`, после чего gracefully останавливает все apps в обратном порядке регистрации.

## Политика перезапуска

По умолчанию упавший app помечается как `failed`, а движок продолжает работу. Для автоматического перезапуска:

```go
rockengine.Register("worker", &WorkerApp{}, rockengine.RestartPolicy{
    MaxRetries: -1,               // -1 = бесконечно, 0 = не перезапускать (по умолчанию), N = до N раз
    Delay:      2 * time.Second,
    OnFatal: func(err error) {   // вызывается когда лимит попыток исчерпан (переопределяет глобальный обработчик)
        log.Printf("worker упал окончательно: %v", err)
    },
})
```

## Собственный Engine

Для большего контроля создайте `Engine` напрямую:

```go
engine := rockengine.NewEngine().
    WithShutdownTimeout(15 * time.Second).
    WithFatalHandler(func(name string, err error) {
        log.Printf("app %s упал: %v", name, err)
    })

engine.Register("worker", &WorkerApp{}, rockengine.RestartPolicy{MaxRetries: 3, Delay: time.Second})
engine.Register("http",   &HTTPApp{})

if err := engine.Run(); err != nil {
    log.Fatal(err)
}
```

## Оркестрирование

Отдельными apps можно управлять во время работы движка, не затрагивая остальные:

```go
// Остановить один app
rockengine.StopApp("worker")

// Перезапустить один app (Stop → Init → Exec)
rockengine.RestartApp("worker")

// Посмотреть статус одного app
info, err := rockengine.AppStatus("worker")
fmt.Println(info.State, info.Retries, info.Err)

// Посмотреть статус всех apps
for _, info := range rockengine.AppStatuses() {
    fmt.Printf("%s: %s\n", info.Name, info.State)
}
```

## Состояния App

| Состояние    | Описание                                             |
|--------------|------------------------------------------------------|
| `idle`       | Зарегистрирован, ещё не запущен                      |
| `running`    | `Exec` активен                                       |
| `restarting` | Ожидание перед следующей попыткой запуска            |
| `stopped`    | Остановлен чисто                                     |
| `failed`     | Исчерпан лимит попыток или фатальная ошибка в `Init` |

## Справочник API

### Пакетные функции (движок по умолчанию)

| Функция                                           | Описание                                   |
|---------------------------------------------------|--------------------------------------------|
| `Register(name, app, policy?)`                   | Зарегистрировать app                       |
| `Run() error`                                     | Запустить и заблокироваться                |
| `RunContext(ctx) error`                           | Запустить с контекстом                     |
| `StopApp(name) error`                             | Остановить один app                        |
| `RestartApp(name) error`                          | Перезапустить один app                     |
| `AppStatus(name) (AppInfo, error)`                | Статус одного app                          |
| `AppStatuses() []AppInfo`                         | Статус всех apps                           |
| `Shutdown() []error`                              | Остановить все apps                        |
| `SetShutdownTimeout(d)`                           | Таймаут остановки (по умолчанию 30s)       |
| `SetFatalHandler(fn)`                             | Обработчик фатального падения app          |

### Методы Engine

Те же, что и выше, плюс:

| Метод                         | Описание                        |
|-------------------------------|---------------------------------|
| `NewEngine() *Engine`         | Создать новый движок            |
| `WithShutdownTimeout(d)`      | Задать таймаут остановки        |
| `WithFatalHandler(fn)`        | Задать обработчик фатальных ошибок |

## Контракт остановки

- Отмена `ctx` сигнализирует `Exec` прекратить приём новой работы и завершить текущие операции.
- `Stop` вызывается сразу после отмены, чтобы разблокировать `Exec` (например, `server.Shutdown`).
- `Stop` не должен форсированно обрывать текущую работу — движок обеспечивает таймаут как крайнюю меру.
- `Stop` должен быть идемпотентным.
- Apps останавливаются в **обратном** порядке регистрации, чтобы соблюдать обратный порядок зависимостей.
