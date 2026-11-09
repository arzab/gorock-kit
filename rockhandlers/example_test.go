package rockhandlers_test

import (
	"context"
	"fmt"
	"time"

	"github.com/arzab/gorock-kit/rockhandlers"
)

type GetOrderParams struct {
	BearerAuth        // satisfies TokenParams → the Bearer step can be used
	ID         string `path:"id"`
}

type GetOrderResult struct {
	ID    string
	Owner string
}

func getOrder(c *rockhandlers.Ctx[*GetOrderParams, *GetOrderResult]) error {
	if c.Params.ID == "" {
		return rockhandlers.NotFound("order not found")
	}
	c.Result.ID = c.Params.ID
	c.Result.Owner = UserKey.MustGet(c).Name
	return nil
}

func logErrors(c rockhandlers.Context) error {
	err := c.Next()
	if err != nil {
		fmt.Printf("%s: %s\n", c.Route(), rockhandlers.KindOf(err))
	}
	return err
}

func Example() {
	route := rockhandlers.Chain("orders.get", Bearer, getOrder).
		With(logErrors, rockhandlers.Timeout(time.Second))

	res, err := rockhandlers.Run[*GetOrderParams, *GetOrderResult](context.Background(), route,
		&GetOrderParams{BearerAuth: BearerAuth{Authorization: "Bearer ann"}, ID: "42"})
	fmt.Println(res.ID, res.Owner, err)

	_, err = rockhandlers.Run[*GetOrderParams, *GetOrderResult](context.Background(), route, &GetOrderParams{ID: "42"})
	fmt.Println(rockhandlers.MessageOf(err))
	// Output:
	// 42 ann <nil>
	// orders.get: unauthorized
	// token required
}
