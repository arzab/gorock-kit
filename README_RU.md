# gorock-kit

Ядро [GOROCK](https://arzab.github.io/gorock/) — структурные части, которые нужны любому GOROCK-сервису независимо от того, что он делает: как он запускается и останавливается и как настраивается.

| Модуль | Описание |
|---|---|
| [rockengine](./rockengine) | Жизненный цикл: сначала ресурсы, затем apps в порядке регистрации, упорядоченная graceful-остановка, политики рестарта, перехват паник, таймаут остановки |
| [rockconfig](./rockconfig) | Один файл конфига (YAML/JSON) → типизированный `Configs`: snake_case, `${ENV:-default}`, значения по умолчанию, проверка обязательных полей и неизвестных ключей |

Go 1.26+. `rockengine` использует только стандартную библиотеку; `rockconfig` добавляет `gopkg.in/yaml.v3`.

HTTP-сервер, сервисы PostgreSQL/Redis, логгер, шина событий, планировщик и Telegram-бот вынесены из kit в [gorock-catalog](https://github.com/arzab/gorock-catalog). Их последние версии здесь, `v0.2.0`, остаются доступны.

## Контракты

Всё, что запускает движок, — одно из двух:

```go
// App — то, что работает: HTTP-сервер, consumer, планировщик.
type App interface {
    Init(ctx context.Context) error
    Exec(ctx context.Context) error // вернуться при отмене ctx
    Stop() []error
}

// Resource — то, чем пользуются apps: пул БД, клиент кэша.
type Resource interface {
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

func main() {
    cfg, err := rockconfig.InitFromFile[Configs]("./configs/configs.yaml")
    if err != nil {
        log.Fatal(err)
    }

    engine := rockengine.NewEngine()
    services.Register(engine, cfg.Services) // ресурсы: init первыми, stop последними
    apps.Register(engine, cfg.Apps)         // apps: останавливаются по одному в обратном порядке

    if err := engine.Run(); err != nil { // блокирует до SIGINT/SIGTERM или падения App
        log.Fatal(err)
    }
}
```

```go
// pkg/services/services.go
type Configs struct {
    Repository repository.Configs
}

var repo repository.Service

func Repository() repository.Service { return repo }

func Register(e *rockengine.Engine, cfg Configs) {
    repo = repository.NewService(cfg.Repository) // реализует rockengine.Resource
    e.MustRegisterResource("repository", repo)
}
```

Куда класть код и почему — [архитектура GOROCK](https://arzab.github.io/gorock/architecture/).

## Установка

```sh
go get github.com/arzab/gorock-kit/rockengine
go get github.com/arzab/gorock-kit/rockconfig
```

## ИИ-агенты

Плагин для Claude Code учит агента структуре GOROCK (skill `gorock:service`):

```sh
claude plugin marketplace add arzab/gorock
claude plugin install gorock@gorock
```

Для Codex, Cursor и других агентов положите [AGENTS.md](https://github.com/arzab/gorock/blob/main/plugin/skills/service/templates/AGENTS.md) в корень проекта. Рецепты плагина пока написаны под полный kit `v0.2.0` и будут обновлены под разделение на ядро и каталог.

## Локальная разработка

`go.work` в корне связывает оба модуля. Корневого модуля нет, поэтому `go build ./...` из корня ничего не находит — используйте скрипт проверки:

```sh
bash scripts/check.sh all                # build + vet + test, все модули
bash scripts/check.sh test rockengine    # один модуль
bash scripts/check.sh tidy-check         # go.mod / go.sum должны быть tidy
```

См. [CHANGELOG.md](./CHANGELOG.md).
