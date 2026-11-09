# gorock-kit

Набор Go-модулей для разработки production-ready приложений. Каждый модуль независим — устанавливай только то, что нужно.

## Модули

| Модуль | Описание |
|---|---|
| [rockengine](./rockengine) | Оркестрация жизненного цикла приложений: инициализация, конкурентный запуск, graceful shutdown, политики перезапуска |
| [rockconfig](./rockconfig) | Загрузка конфигурации из JSON/YAML с автоматическим маппингом snake_case, подстановкой env-переменных и валидацией |
| [rocklog](./rocklog) | Интерфейс структурированного логирования с бэкендом на `log/slog` и глобальным/инстансным использованием |
| [rockfiber](./rockfiber) | HTTP-сервер на Fiber с типизированными эндпоинтами, мидлварами и интеграцией с rockengine |
| [rockredis](./rockredis) | Обёртка Redis-клиента (go-redis v9) с типизированным интерфейсом Service |
| [rockbun](./rockbun) | Обёртка PostgreSQL (bun ORM) с настройкой пула соединений и хелперами для транзакций |
| [rockbus](./rockbus) | Внутрипроцессная шина событий с упорядоченной доставкой по топикам и интеграцией с rockengine |
| [rockcron](./rockcron) | Планировщик задач с cron-выражениями, интервальными джобами и цепочками хендлеров |
| [rocktelebot](./rocktelebot) | Обёртка Telegram-бота со структурированными хендлерами, встроенными мидлварами и билдерами клавиатур |

## Архитектура

- **Модули не импортируют друг друга** — ставьте только нужные. Компоненты с жизненным циклом (`rockfiber`, `rockbus`, `rockcron`, `rocktelebot`) реализуют `rockengine.App` (`Init / Exec / Stop`) по сигнатуре, а обёртки БД и кэша (`rockbun`, `rockredis`) — `rockengine.Resource` (`Init / Stop`).
- **rockengine** их запускает: сначала ресурсы, затем apps в порядке регистрации; при остановке apps останавливаются по одному в обратном порядке, после чего закрываются ресурсы.
- **rockconfig** загружает один типизированный файл конфига; каждый модуль предоставляет структуру `Config`/`Configs`, которая в него встраивается.
- **rocklog** — структурированный логгер. Модули сообщают об ошибках через колбэки (`OnError`, `onError`), поэтому можно подключить rocklog или любой другой логгер.

```
rockengine ── ресурсы: rockbun, rockredis       (init первыми, stop последними)
           └─ apps:    rockbus, rockcron,
                       rockfiber, rocktelebot   (stop в обратном порядке)

rockconfig ──► один файл → Config каждого модуля
rocklog    ◄── подключается через колбэки OnError и rocklog.WithContext
```

## Пример: полное приложение

Полная рабочая версия — [`examples/fullapp`](./examples/fullapp). Она собирается в CI, поэтому этот фрагмент не может разойтись с API.

```go
func main() {
    cfg, err := rockconfig.InitFromFile[AppConfig]("config.yaml")
    if err != nil {
        log.Fatal(err)
    }

    rocklog.Init(rocklog.Config{Level: rocklog.LevelInfo, Format: rocklog.FormatJSON})

    engine := rockengine.NewEngine()

    // Ресурсы: инициализируются до всех apps, закрываются после них.
    db := rockbun.NewService(cfg.DB)
    engine.MustRegisterResource("db", db)
    engine.MustRegisterResource("redis", rockredis.NewService(cfg.Redis))

    // Шина событий
    bus := rockbus.NewApp(cfg.Bus,
        func(ctx context.Context, event rockbus.Event, err error) { /* обработка */ },
        rockbus.On("user.created", onUserCreated),
    )
    rockbus.SetDefault(bus)
    engine.MustRegister("bus", bus)

    // Планировщик
    cron := rockcron.NewApp(cfg.Cron,
        func(ctx context.Context, job rockcron.Job, err error) { /* обработка */ },
        rockcron.Every("ping-db", time.Minute, func(ctx context.Context) error { return db.Ping(ctx) }),
    )
    engine.MustRegister("cron", cron)

    // HTTP-сервер — зарегистрирован последним, поэтому останавливается первым.
    server := rockfiber.New(cfg.HTTP,
        rockfiber.GET("/health", healthHandler),
        rockfiber.POST("/users", createUserHandler),
    )
    engine.MustRegister("http", server, rockengine.RestartPolicy{MaxRetries: 3, Delay: time.Second})

    if err := engine.Run(); err != nil { // блокирует до SIGINT/SIGTERM или падения app
        log.Fatal(err)
    }
}
```

## Установка

Каждый модуль — отдельный Go-модуль. Устанавливай только нужное:

```sh
go get github.com/arzab/gorock-kit/rockengine
go get github.com/arzab/gorock-kit/rockconfig
go get github.com/arzab/gorock-kit/rocklog
go get github.com/arzab/gorock-kit/rockfiber
go get github.com/arzab/gorock-kit/rockredis
go get github.com/arzab/gorock-kit/rockbun
go get github.com/arzab/gorock-kit/rockbus
go get github.com/arzab/gorock-kit/rockcron
go get github.com/arzab/gorock-kit/rocktelebot
```

## ИИ-агенты

Плагин для Claude Code учит агента структуре GOROCK и этому API (skill `gorock:service`: создать проект, добавить endpoints, сервисы, события, задачи, apps — и проверить сборку):

```sh
claude plugin marketplace add arzab/gorock
claude plugin install gorock@gorock
```

Для Codex, Cursor и других агентов положите [AGENTS.md](https://github.com/arzab/gorock/blob/main/plugin/skills/service/templates/AGENTS.md) в корень проекта. Подробнее: [arzab.github.io/gorock/ai](https://arzab.github.io/gorock/ai/).

## Локальная разработка

`go.work` в корне связывает все модули (и `examples/fullapp`). Корневого модуля нет, поэтому `go build ./...` из корня ничего не находит — используйте скрипт проверки, он проходит по модулям:

```sh
bash scripts/check.sh all                # build + vet + test, все модули
bash scripts/check.sh test rockengine    # один модуль
bash scripts/check.sh tidy-check         # go.mod / go.sum должны быть tidy
```

Тесты запускаются с `-race`, если тулчейн это поддерживает (нужны cgo и C-компилятор). Изменения поведения — в [CHANGELOG.md](./CHANGELOG.md).
