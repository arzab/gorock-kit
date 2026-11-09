package rockbus_test

import (
	"context"
	"errors"
	"log"

	"github.com/arzab/gorock-kit/rockbus"
)

// Compiled, not run: mirrors the README.

type UserCreated struct{ UserID string }

func onUserCreated(ctx context.Context) error {
	p, err := rockbus.Payload[UserCreated](ctx)
	if err != nil {
		return err
	}
	log.Printf("user %s created (topic %s)", p.UserID, rockbus.CurrentTopic(ctx))
	return nil
}

func ExampleNewApp() {
	subscriptions := []rockbus.Subscription{
		rockbus.On("user.created", onUserCreated),
	}
	onError := func(ctx context.Context, event rockbus.Event, err error) {
		switch {
		case errors.Is(err, rockbus.ErrQueueFull), errors.Is(err, rockbus.ErrAppStopped):
			log.Printf("dropped %s: %v", event.Topic, err)
		default:
			log.Printf("bus error on %s: %v", event.Topic, err)
		}
	}

	app := rockbus.NewApp(rockbus.Config{QueueSize: 1024}, onError, subscriptions...)
	if err := app.Subscribe("user.created", onUserCreated); err != nil {
		log.Fatal(err)
	}
	rockbus.SetDefault(app)

	ctx := context.Background()
	if err := app.Init(ctx); err != nil {
		log.Fatal(err)
	}
	rockbus.PublishAsync(ctx, rockbus.Event{Topic: "user.created", Payload: UserCreated{UserID: "42"}})
	if err := rockbus.Publish(ctx, rockbus.Event{Topic: "user.created", Payload: UserCreated{UserID: "43"}}); err != nil {
		log.Print(err)
	}
}
