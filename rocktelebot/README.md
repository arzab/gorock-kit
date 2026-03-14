# rocktelebot

A Telegram bot wrapper built on [telebot.v3](https://gopkg.in/telebot.v3) that integrates with [rockengine](../rockengine) and provides structured handler registration, built-in middlewares, and keyboard builders.

Satisfies the rockengine `App` interface (`Init / Exec / Stop`).

## Quick Start

```go
app := rocktelebot.NewApp(cfg,
    []tele.MiddlewareFunc{
        rocktelebot.Recovery(func(c tele.Context, err interface{}) {
            log.Error("panic", rocklog.Any("err", err))
        }),
        rocktelebot.Logger(func(c tele.Context) {
            log.Info("update", rocklog.Int64("user_id", c.Sender().ID), rocklog.Str("text", c.Text()))
        }),
    },
    rocktelebot.Command("/start", onStart).WithDesc("Start the bot"),
    rocktelebot.Command("/help",  onHelp).WithDesc("Help"),
    rocktelebot.GetHandler(tele.OnText,     onText),
    rocktelebot.GetHandler(tele.OnCallback, onCallback),
)

engine.MustRegister("bot", app, rockengine.RestartPolicy{})
engine.Run()
```

## Config

```go
type Config struct {
    // Telegram Bot API token from @BotFather. Required.
    Token string

    // Webhook configures webhook mode.
    // If Listen is empty, long polling is used.
    Webhook WebhookConfig `config:",omitempty"`
}

type WebhookConfig struct {
    // Address the webhook server listens on, e.g. ":8443".
    Listen string `config:",omitempty"`

    // Public HTTPS URL Telegram sends updates to, e.g. "https://example.com/bot".
    PublicURL string `config:"public_url,omitempty"`
}
```

```yaml
# config.yaml
token: "123456:ABC-DEF..."

# optional — long polling is used by default
webhook:
  listen: ":8443"
  public_url: "https://example.com/bot"
```

## Lifecycle

```
NewApp(cfg, middlewares, handlers...) → Init → Exec (blocks) → Stop
```

- **Init** creates the bot, registers middlewares and handlers, pushes command descriptions to Telegram.
- **Exec** starts polling/webhook and blocks until `ctx` is cancelled or `Stop` is called.
- **Stop** is idempotent and safe to call before `Init`.

## Handlers

### Command

Triggered when the user sends a bot command.

```go
rocktelebot.Command("/start", onStart)
rocktelebot.Command("/help",  onHelp, authMiddleware) // with per-handler middleware
```

### GetHandler

Triggered for any telebot entity — text, media, callbacks, and more.

```go
rocktelebot.GetHandler(tele.OnText,        onText)
rocktelebot.GetHandler(tele.OnPhoto,       onPhoto)
rocktelebot.GetHandler(tele.OnDocument,    onDocument)
rocktelebot.GetHandler(tele.OnVoice,       onVoice)
rocktelebot.GetHandler(tele.OnCallback,    onCallback)
rocktelebot.GetHandler(tele.OnUserJoined,  onUserJoined)
rocktelebot.GetHandler(tele.OnQuery,       onInlineQuery)
```

All available entities are defined in the telebot package as `tele.OnXxx` constants.

### Commands menu

Add `.WithDesc` to a `Command` to show it in the Telegram bot menu (the list that appears when the user types `/`):

```go
rocktelebot.Command("/start",  onStart).WithDesc("Start the bot")
rocktelebot.Command("/help",   onHelp).WithDesc("Show help")
rocktelebot.Command("/cancel", onCancel).WithDesc("Cancel current action")
```

Descriptions are pushed to Telegram automatically during `Init`.

## Middlewares

Middlewares run before every handler. Pass them as the second argument to `NewApp` to apply globally.

### Built-in

**Recovery** — catches panics in handlers so the bot never crashes:

```go
rocktelebot.Recovery(func(c tele.Context, err interface{}) {
    log.Error("handler panic", rocklog.Any("err", err))
})

// nil = write panic to stderr
rocktelebot.Recovery(nil)
```

**Logger** — called for every incoming update before the handler:

```go
rocktelebot.Logger(func(c tele.Context) {
    log.Info("update",
        rocklog.Int64("user_id", c.Sender().ID),
        rocklog.Str("text", c.Text()),
    )
})
```

### Per-handler middleware

Pass middlewares as extra arguments to `Command` or `GetHandler`:

```go
rocktelebot.Command("/admin", onAdmin, adminOnly)
rocktelebot.GetHandler(tele.OnText, onText, rateLimiter, logMiddleware)
```

## Keyboards

### Inline keyboard

Buttons attached to a specific message. Pressing a button generates a callback update.

```go
kb := rocktelebot.NewInlineKeyboard()

c.Send("Confirm?", kb.
    Row(kb.Data("✅ Yes", "confirm"), kb.Data("❌ No", "cancel")).
    Row(kb.URL("Open site", "https://example.com")).
    Markup(),
)

// Handle the callback
rocktelebot.GetHandler(tele.OnCallback, func(c tele.Context) error {
    data := c.Callback().Data // "confirm" or "cancel"
    return c.Respond()        // removes the loading spinner from the button
})
```

### Reply keyboard

Buttons replace the user's keyboard at the bottom of the screen. Pressing sends text.

```go
kb := rocktelebot.NewReplyKeyboard()

c.Send("Choose section:", kb.
    Row(kb.Text("📋 List"), kb.Text("➕ Add")).
    Row(kb.Text("❌ Cancel")).
    Markup(),
)

// Remove the keyboard
c.Send("Done", rocktelebot.RemoveKeyboard())

// Hide after one press
kb.OneTime()
```

## Polling vs Webhook

| Mode | When to use |
|---|---|
| Long polling (default) | Development, small bots, simple deployments |
| Webhook | Production, high load, when you already have an HTTPS server |

Long polling requires no infrastructure — the bot connects to Telegram itself.
Webhook requires a public HTTPS endpoint and a valid TLS certificate.
