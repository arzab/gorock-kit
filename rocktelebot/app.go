package rocktelebot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"
)

// App is the rocktelebot Telegram bot runner.
// It satisfies the rockengine App interface (Init / Exec / Stop).
//
// Usage:
//
//	app := rocktelebot.NewApp(cfg,
//	    []tele.MiddlewareFunc{rocktelebot.Recovery(nil), rocktelebot.Logger(logFn)},
//	    rocktelebot.Command("/start", onStart).WithDesc("Start the bot"),
//	    rocktelebot.Command("/help",  onHelp).WithDesc("Help"),
//	    rocktelebot.GetHandler(tele.OnText,     onText),
//	    rocktelebot.GetHandler(tele.OnCallback, onCallback),
//	)
//	engine.MustRegister("bot", app, rockengine.RestartPolicy{})
type App struct {
	cfg         Config
	middlewares []tele.MiddlewareFunc
	handlers    []Handler
	bot         *tele.Bot
}

// NewApp creates an App with the given global middlewares and handlers.
// middlewares can be nil.
func NewApp(cfg Config, middlewares []tele.MiddlewareFunc, handlers ...Handler) *App {
	return &App{
		cfg:         cfg,
		middlewares: middlewares,
		handlers:    handlers,
	}
}

// Init creates the bot, applies middlewares, and registers all handlers.
// Safe to call again after Stop — resets internal state for restart.
func (a *App) Init(_ context.Context) error {
	var poller tele.Poller
	if a.cfg.Webhook.Listen != "" {
		poller = &tele.Webhook{
			Listen:   a.cfg.Webhook.Listen,
			Endpoint: &tele.WebhookEndpoint{PublicURL: a.cfg.Webhook.PublicURL},
		}
	} else {
		poller = &tele.LongPoller{Timeout: 10 * time.Second}
	}

	bot, err := tele.NewBot(tele.Settings{
		Token:  a.cfg.Token,
		Poller: poller,
	})
	if err != nil {
		return fmt.Errorf("rocktelebot: create bot: %w", err)
	}

	if len(a.middlewares) > 0 {
		bot.Use(a.middlewares...)
	}

	// Register handlers and collect command descriptions for the Telegram menu.
	var cmds []tele.Command
	for _, h := range a.handlers {
		bot.Handle(h.entity, h.handler, h.middlewares...)
		if cmd, ok := h.entity.(string); ok && h.desc != "" {
			cmds = append(cmds, tele.Command{
				Text:        strings.TrimPrefix(cmd, "/"),
				Description: h.desc,
			})
		}
	}

	if len(cmds) > 0 {
		if err := bot.SetCommands(cmds); err != nil {
			return fmt.Errorf("rocktelebot: set commands: %w", err)
		}
	}

	a.bot = bot
	return nil
}

// Exec starts the bot and blocks until ctx is cancelled or Stop is called.
func (a *App) Exec(ctx context.Context) error {
	if a.bot == nil {
		return fmt.Errorf("rocktelebot: Init must be called before Exec")
	}

	go func() {
		<-ctx.Done()
		a.bot.Stop()
	}()

	a.bot.Start()
	return nil
}

// Stop shuts down the bot. Safe to call before Init.
func (a *App) Stop() []error {
	if a.bot == nil {
		return nil
	}
	a.bot.Stop()
	return nil
}
