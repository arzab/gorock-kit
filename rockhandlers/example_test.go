package rockhandlers_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/arzab/gorock-kit/rockhandlers"
)

// --- internal/handlers/tasks ---

type CreateParams struct {
	Token string `header:"Authorization"`
	Title string `json:"title"`
}

func (p *CreateParams) Validate() error {
	if strings.TrimSpace(p.Title) == "" {
		return rockhandlers.Invalid("invalid task").WithFields(map[string]string{"title": "required"})
	}
	return nil
}

type CreateResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (r *CreateResponse) Normalize() error { return nil }

func checkAuth(c *rockhandlers.Ctx, params *CreateParams, _ *CreateResponse) error {
	if params.Token == "" {
		return rockhandlers.Unauthorized("token required")
	}
	return c.Next() // go on
}

func create(_ *rockhandlers.Ctx, params *CreateParams, response *CreateResponse) error {
	response.ID, response.Title = "42", params.Title
	return nil // done
}

var Create = rockhandlers.Chain(checkAuth, create)

// --- an App's middleware ---

func logErrors(c *rockhandlers.Ctx, _ rockhandlers.Params, _ rockhandlers.Response) error {
	err := c.Next()
	if err != nil {
		fmt.Println("failed:", rockhandlers.KindOf(err))
	}
	return err
}

func Example() {
	pipeline := Create.Use(logErrors)

	res, err := pipeline.Call(context.Background(), &CreateParams{Token: "t", Title: "write docs"})
	fmt.Println(res.(*CreateResponse).ID, err)

	_, err = pipeline.Call(context.Background(), &CreateParams{Title: "write docs"})
	fmt.Println(rockhandlers.MessageOf(err))
	// Output:
	// 42 <nil>
	// failed: unauthorized
	// token required
}
