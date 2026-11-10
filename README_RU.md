# gorock-kit

Ядро [GOROCK](https://arzab.github.io/gorock/) — структурные части, которые нужны любому GOROCK-сервису независимо от того, что он делает: как он запускается и останавливается и как настраивается.

| Модуль | Описание |
|---|---|
| [rockengine](./rockengine) | Жизненный цикл App: запуск в заданном порядке, остановка по одному в обратном, перехват паник, таймаут остановки, перезапуски по желанию |
| [rockconfig](./rockconfig) | Один файл конфига (YAML/JSON) → типизированный `Configs`: snake_case, `${ENV:-default}`, значения по умолчанию, проверка обязательных полей и неизвестных ключей |
| [rockservices](./rockservices) | Слой сервисов: один каталог, который создаёт, хранит по названию, выдаёт, инициализирует и останавливает сервисы `pkg/services` |
| [rockhandlers](./rockhandlers) | Обработка запросов без привязки к транспорту: цепочка типизированных шагов с `Next`, Locals, middleware, валидацией, перехватом паник и видами ошибок — любой транспорт (HTTP, очередь, CLI) запускает одни и те же маршруты |

Go 1.26+. `rockengine`, `rockservices` и `rockhandlers` используют только стандартную библиотеку; `rockconfig` добавляет `gopkg.in/yaml.v3`.

HTTP-сервер, сервисы PostgreSQL/Redis, логгер, шина событий, планировщик и Telegram-бот находятся в [gorock-catalog](https://github.com/arzab/gorock-catalog), там же `rockhttp` — HTTP-транспорт для rockhandlers. Их последние версии внутри kit, `v0.2.0`, остаются доступны.

## Как они связаны

```
cmd/main:  rockconfig → services.Init → rockengine.Run (apps) → services.Stop
```

Сервисы — отдельный слой: стартуют до apps и останавливаются после них. Движок запускает только apps:

```go
// App: HTTP-сервер, consumer, планировщик, бот
type App interface {
    Init(ctx context.Context) error
    Exec(ctx context.Context) error // вернуться при отмене ctx
    Stop() []error
}

// сервис в pkg/services: БД, кэш, внешний API
type Service interface {
    Init(ctx context.Context) error
    Stop() error
}
```

## Пример

```go
type Configs struct {
    Services services.Configs
    Apps     apps.Configs
}

func Exec(ctx context.Context) error {
    cfg, err := rockconfig.InitFromFile[Configs]("./configs/configs.yaml")
    if err != nil {
        return err
    }

    if err := services.Init(ctx, cfg.Services); err != nil {
        return err
    }
    defer services.Stop()

    return apps.Run(ctx, cfg.Apps) // rockengine: до SIGINT/SIGTERM или падения App
}
```

```go
// pkg/services/services.go
const RepositoryName = "repository"

var catalog *rockservices.Catalog

func Init(ctx context.Context, cfg Configs) error {
    catalog = rockservices.New(map[string]rockservices.Service{
        RepositoryName: repository.NewService(cfg.Repository),
    })
    return catalog.Init(ctx, RepositoryName)
}

func Stop() error { return catalog.Stop() }

func Repository() repository.Service { return catalog.Get(RepositoryName).(repository.Service) }

// где угодно: services.Repository().Insert(ctx, task)
```

Куда класть код и почему — [архитектура GOROCK](https://arzab.github.io/gorock/architecture/).

## Установка

```sh
go get github.com/arzab/gorock-kit/rockengine
go get github.com/arzab/gorock-kit/rockconfig
go get github.com/arzab/gorock-kit/rockservices
go get github.com/arzab/gorock-kit/rockhandlers
```

## ИИ-агенты

Плагин для Claude Code учит агента структуре GOROCK (skill `gorock:service`):

```sh
claude plugin marketplace add arzab/gorock
claude plugin install gorock@gorock
```

Для Codex, Cursor и других агентов положите [AGENTS.md](https://github.com/arzab/gorock/blob/main/plugin/skills/service/templates/AGENTS.md) в корень проекта.

## Локальная разработка

`go.work` в корне связывает модули. Корневого модуля нет, поэтому `go build ./...` из корня ничего не находит — используйте скрипт проверки:

```sh
bash scripts/check.sh all                # build + vet + test, все модули
bash scripts/check.sh test rockengine    # один модуль
bash scripts/check.sh tidy-check         # go.mod / go.sum должны быть tidy
```

См. [CHANGELOG.md](./CHANGELOG.md).
