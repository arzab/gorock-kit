package rocktelebot_test

import (
	"context"
	"log"

	"github.com/arzab/gorock-kit/rocktelebot"
	tele "gopkg.in/telebot.v3"
)

// Compiled, not run: mirrors the README Quick Start.

func ExampleNewApp() {
	onStart := func(c tele.Context) error {
		kb := rocktelebot.NewInlineKeyboard()
		return c.Send("Choose:", kb.
			Row(kb.Data("Yes", "confirm"), kb.Data("No", "cancel")).
			Row(kb.URL("Open site", "https://example.com")).
			Markup())
	}
	onText := func(c tele.Context) error { return c.Send("echo: " + c.Text()) }

	app := rocktelebot.NewApp(rocktelebot.Config{Token: "123:abc"},
		[]tele.MiddlewareFunc{
			rocktelebot.Recovery(func(c tele.Context, err interface{}) { log.Printf("panic: %v", err) }),
			rocktelebot.Logger(func(c tele.Context) { log.Printf("update from %d", c.Sender().ID) }),
		},
		rocktelebot.Command("/start", onStart).WithDesc("Start the bot"),
		rocktelebot.GetHandler(tele.OnText, onText),
	)

	ctx := context.Background()
	if err := app.Init(ctx); err != nil {
		log.Fatal(err)
	}
	if err := app.Exec(ctx); err != nil {
		log.Fatal(err)
	}
}
