package rockdelivery_test

import (
	"context"
	"fmt"
	"time"

	"github.com/arzab/gorock-kit/rockdelivery"
)

// internal/delivery/orders/create.go — business logic, no transport.

type CreateParams struct {
	Item string `json:"item"`
	Qty  int    `json:"qty"`
}

func (p *CreateParams) Validate() error {
	if p.Qty <= 0 {
		return rockdelivery.Invalid("qty must be > 0")
	}
	return nil
}

type CreateResult struct {
	ID string `json:"id"`
}

func Create(ctx context.Context, p *CreateParams) (*CreateResult, error) {
	if p.Item == "unknown" {
		return nil, rockdelivery.NotFound("item %q not found", p.Item)
	}
	return &CreateResult{ID: "order-1"}, nil
}

// What any App does with it: decode → Call → map the result and the error kind.
func Example() {
	action := rockdelivery.Bind(Create, rockdelivery.Timeout(5*time.Second))

	in := action.NewInput().(*CreateParams) // a real App decodes JSON, a message, flags... into it
	in.Item, in.Qty = "book", 0

	out, err := action.Call(context.Background(), in)
	if err != nil {
		fmt.Println(rockdelivery.KindOf(err), rockdelivery.MessageOf(err))
		return
	}
	fmt.Println(out.(*CreateResult).ID)
	// Output: invalid qty must be > 0
}
