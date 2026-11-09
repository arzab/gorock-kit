// Command fullapp wires every gorock-kit module into one service.
// It is the source of the example in the root README and is built in CI,
// so the README cannot drift from the real API.
//
// Run it from this directory (needs Postgres and Redis; see config.yaml):
//
//	go run .
package main

import (
	"context"
	"log"
	"time"

	"github.com/arzab/gorock-kit/rockbun"
	"github.com/arzab/gorock-kit/rockbus"
	"github.com/arzab/gorock-kit/rockconfig"
	"github.com/arzab/gorock-kit/rockcron"
	"github.com/arzab/gorock-kit/rockengine"
	"github.com/arzab/gorock-kit/rockfiber"
	"github.com/arzab/gorock-kit/rocklog"
	"github.com/arzab/gorock-kit/rockredis"
	"github.com/gofiber/fiber/v2"
)

type AppConfig struct {
	HTTP  rockfiber.Config
	Bus   rockbus.Config    `config:",omitempty"`
	Cron  rockcron.Config   `config:",omitempty"`
	DB    rockbun.Configs   `config:",omitempty"`
	Redis rockredis.Configs `config:",omitempty"`
}

type UserCreated struct{ ID string }

func main() {
	cfg, err := rockconfig.InitFromFile[AppConfig]("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	rocklog.Init(rocklog.Config{Level: rocklog.LevelInfo, Format: rocklog.FormatJSON})

	engine := rockengine.NewEngine()

	// Resources: initialised before every app, closed after every app.
	db := rockbun.NewService(cfg.DB)
	cache := rockredis.NewService(cfg.Redis)
	engine.MustRegisterResource("db", db)
	engine.MustRegisterResource("redis", cache)

	// Event bus
	bus := rockbus.NewApp(cfg.Bus,
		func(ctx context.Context, event rockbus.Event, err error) {
			rocklog.FromContext(ctx).Error("bus error", rocklog.Str("topic", string(event.Topic)), rocklog.Err(err))
		},
		rockbus.On("user.created", onUserCreated),
	)
	rockbus.SetDefault(bus)
	engine.MustRegister("bus", bus)

	// Scheduler
	cron := rockcron.NewApp(cfg.Cron,
		func(ctx context.Context, job rockcron.Job, err error) {
			rocklog.Error("cron error", rocklog.Str("job", rockcron.JobName(job)), rocklog.Err(err))
		},
		rockcron.Every("ping-db", time.Minute, func(ctx context.Context) error { return db.Ping(ctx) }),
	)
	engine.MustRegister("cron", cron)

	// HTTP server — registered last, so it is stopped first.
	cfg.HTTP.OnError = func(traceId string, e *rockfiber.ErrorResponse) {
		rocklog.Error("http error", rocklog.Str("trace_id", traceId), rocklog.Str("error", e.Error()))
	}
	server := rockfiber.New(cfg.HTTP,
		rockfiber.GET("/health", func(c *fiber.Ctx) error { return c.SendString("ok") }),
		rockfiber.POST("/users", createUser),
	)
	engine.MustRegister("http", server, rockengine.RestartPolicy{MaxRetries: 3, Delay: time.Second, ResetAfter: time.Minute})

	if err := engine.Run(); err != nil {
		log.Fatal(err)
	}
}

func createUser(c *fiber.Ctx) error {
	ctx := rocklog.WithContext(c.UserContext(), rocklog.Str("trace_id", rockfiber.TraceIdFromContext(c.UserContext())))
	rockbus.PublishAsync(ctx, rockbus.Event{Topic: "user.created", Payload: UserCreated{ID: "42"}})
	return c.SendStatus(fiber.StatusAccepted)
}

func onUserCreated(ctx context.Context) error {
	u, err := rockbus.Payload[UserCreated](ctx)
	if err != nil {
		return err
	}
	rocklog.FromContext(ctx).Info("user created", rocklog.Str("id", u.ID))
	return nil
}
