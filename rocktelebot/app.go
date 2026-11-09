package rocktelebot

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

	mu      sync.Mutex
	bot     *tele.Bot
	stopped bool          // Stop was requested for the current Init generation
	done    chan struct{} // closed when the current bot.Start returns; nil when not running
	stopBot func()        // stops the current run exactly once; nil when not running
}

// newBot is overridable in tests to create an offline bot with a fake poller.
var newBot = tele.NewBot

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

	bot, err := newBot(tele.Settings{
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

	a.mu.Lock()
	a.bot = bot
	a.stopped = false
	a.mu.Unlock()
	return nil
}

// Exec starts the bot and blocks until ctx is cancelled or Stop is called.
//
// Cancellation and Stop may happen concurrently and any number of times:
// the underlying bot is stopped exactly once per run. telebot's Bot.Stop blocks
// forever when the bot is not running, so it must never be called twice.
func (a *App) Exec(ctx context.Context) error {
	a.mu.Lock()
	bot := a.bot
	if bot == nil {
		a.mu.Unlock()
		return fmt.Errorf("rocktelebot: Init must be called before Exec")
	}
	if a.done != nil {
		a.mu.Unlock()
		return fmt.Errorf("rocktelebot: Exec is already running")
	}
	if a.stopped {
		// Stop arrived between Init and Exec — do not start.
		a.mu.Unlock()
		return nil
	}
	done := make(chan struct{})
	a.done = done
	a.stopBot = sync.OnceFunc(bot.Stop)
	a.mu.Unlock()

	go func() {
		defer close(done)
		bot.Start()
	}()

	select {
	case <-ctx.Done():
		a.stop()
	case <-done:
	}
	<-done

	a.mu.Lock()
	a.done = nil
	a.stopBot = nil
	a.mu.Unlock()
	return nil
}

// Stop shuts down the bot and waits for Exec to finish processing updates.
// Safe to call before Init, before Exec, concurrently with ctx cancellation,
// and multiple times.
func (a *App) Stop() []error {
	a.stop()
	return nil
}

func (a *App) stop() {
	a.mu.Lock()
	a.stopped = true
	stopBot, done := a.stopBot, a.done
	a.mu.Unlock()

	if stopBot == nil {
		return // not running
	}
	stopBot() // concurrent callers block here until the first call completes
	<-done
}
