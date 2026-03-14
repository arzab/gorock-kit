# gorock-kit

Набор Go-модулей для разработки production-ready приложений. Каждый модуль независим — устанавливай только то, что нужно.

## Модули

| Модуль | Описание |
|---|---|
| [rockengine](./rockengine) | Оркестрация жизненного цикла приложений: инициализация, конкурентный запуск, graceful shutdown, политики перезапуска |
| [rockconfig](./rockconfig) | Загрузка конфигурации из JSON/YAML с автоматическим маппингом snake_case, подстановкой env-переменных и валидацией |
| [rocklog](./rocklog) | Интерфейс структурированного логирования с logrus-бекендом и глобальным/инстансным использованием |
| [rockfiber](./rockfiber) | HTTP-сервер на Fiber с типизированными эндпоинтами, мидлварами и интеграцией с rockengine |
| [rockredis](./rockredis) | Обёртка Redis-клиента (go-redis v9) с типизированным интерфейсом Service |
| [rockbun](./rockbun) | Обёртка PostgreSQL (bun ORM) с настройкой пула соединений и хелперами для транзакций |
| [rockbus](./rockbus) | Внутрипроцессная шина событий с упорядоченной доставкой по топикам и интеграцией с rockengine |

## Архитектура

Все модули следуют единому подходу:

- **rockengine** — основа. Любой компонент, которому нужен управляемый жизненный цикл, реализует `App` (`Init / Exec / Stop`) и регистрируется в движке.
- **rockconfig** загружает типизированные конфиги из файла; каждый модуль предоставляет свой `Config`/`Configs`, совместимый с маппингом rockconfig.
- **rocklog** обеспечивает структурированное логирование во всех модулях.

```
rockengine
    ├── rockfiber   (HTTP-сервер)
    ├── rockbus     (шина событий)
    └── твои сервисы

rockconfig  ──►  конфиги всех модулей
rocklog     ──►  все модули
```

## Пример: полное приложение

```go
func main() {
    cfg, err := rockconfig.InitFromFile[AppConfig]("config.yaml")
    if err != nil {
        log.Fatal(err)
    }

    rocklog.Init(rocklog.Config{Level: rocklog.LevelInfo, Format: rocklog.FormatJSON})

    engine := rockengine.New()

    // HTTP-сервер
    server := rockfiber.New(cfg.HTTP,
        rockfiber.GET("/health", healthHandler),
        rockfiber.POST("/users", createUserHandler),
    )
    engine.MustRegister("http", server, rockengine.RestartPolicy{})

    // Шина событий
    bus := rockbus.NewApp(cfg.Bus,
        rockbus.On("user.created", onUserCreated),
        rockbus.On("order.placed", onOrderPlaced),
    )
    rockbus.SetDefault(bus)
    engine.MustRegister("bus", bus, rockengine.RestartPolicy{})

    engine.Run()
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
```

## Локальная разработка

Файл `go.work` в корне репозитория связывает все модули для локальной разработки:

```sh
go work sync
go build ./...
```
