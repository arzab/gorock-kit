package rockfiber_test

import (
	"context"
	"log"
	"net/http"

	"github.com/arzab/gorock-kit/rockfiber"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

// Compiled, not run: mirrors the README Quick Start.

type CreateUserParams struct {
	Name string `json:"name"`
}

func (p *CreateUserParams) Validate(*fiber.Ctx) error {
	if p.Name == "" {
		return rockfiber.NewError(http.StatusBadRequest, "name is required")
	}
	return nil
}

func ExampleNew() {
	helloHandler := func(c *fiber.Ctx) error { return c.SendString("hello") }
	createUser := func(c *fiber.Ctx) error {
		p, err := rockfiber.GetFromContext[CreateUserParams](c, "params")
		if err != nil {
			return err
		}
		traceId := rockfiber.TraceIdFromContext(c.UserContext())
		return c.JSON(map[string]string{"name": p.Name, "trace_id": traceId})
	}

	app := rockfiber.New(
		rockfiber.Config{
			Port:                           "8080",
			UseTraceId:                     true,
			Compress:                       true,
			Pprof:                          true,
			AdminPassword:                  "secret",
			MaskInternalServerErrorMessage: "internal server error",
			CorsConfig:                     &cors.Config{AllowOrigins: "https://app.example.com"},
			OnError: func(traceId string, err *rockfiber.ErrorResponse) {
				log.Printf("[%s] %v", traceId, err)
			},
		},
		rockfiber.GET("/hello", helloHandler),
		rockfiber.POST("/users", rockfiber.DefaultHandler[CreateUserParams](), createUser),
	)

	ctx := context.Background()
	if err := app.Init(ctx); err != nil {
		log.Fatal(err)
	}
	if err := app.Exec(ctx); err != nil {
		log.Fatal(err)
	}
}
