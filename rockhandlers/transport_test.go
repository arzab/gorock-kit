package rockhandlers_test

import (
	"context"
	"encoding/json"
	"testing"

	rh "github.com/arzab/gorock-kit/rockhandlers"
)

// A toy queue transport: it proves the same route runs on a non-HTTP
// transport. It fills Params from a message (headers + JSON body) and turns
// the outcome into ack / reject / requeue.

type message struct {
	Headers map[string]string
	Body    string
}

type outcome string

func consume(route rh.Route, m message) (outcome, any) {
	p := route.NewParams()
	if err := json.Unmarshal([]byte(m.Body), p); err != nil {
		return "reject", nil
	}
	if a, ok := p.(interface{ setAuth(string) }); ok { // the transport's own binding
		a.setAuth(m.Headers["authorization"])
	}
	res, err := route.Serve(context.Background(), p)
	switch rh.KindOf(err) {
	case rh.KindInternal, rh.KindUnavailable, rh.KindCanceled, rh.KindTimeout:
		if err != nil {
			return "requeue", nil
		}
		return "ack", res
	default:
		return "reject", nil
	}
}

func (a *BearerAuth) setAuth(v string) { a.Authorization = v }

func TestSameRouteOnQueueTransport(t *testing.T) {
	route := Secured("orders.create", create) // the very same route as in route_test.go

	cases := []struct {
		m    message
		want outcome
	}{
		{message{map[string]string{"authorization": "Bearer ann"}, `{"Item":"tea","Qty":2}`}, "ack"},
		{message{nil, `{"Item":"tea","Qty":2}`}, "reject"},                                      // Unauthorized
		{message{map[string]string{"authorization": "Bearer ann"}, `{"Item":"tea"}`}, "reject"}, // Invalid
		{message{nil, `not json`}, "reject"},
	}
	for _, c := range cases {
		got, res := consume(route, c.m)
		if got != c.want {
			t.Fatalf("%+v: got %s, want %s", c.m, got, c.want)
		}
		if got == "ack" && res.(*CreateResult).Owner != "ann" {
			t.Fatalf("result %+v", res)
		}
	}
}
