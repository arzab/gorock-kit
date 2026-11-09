package rockdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// A fake transport: requests are JSON strings, answers go to a recorder.
// Its success status comes from route metadata, like an HTTP transport would.

var successStatus = NewMetaKey[int]("fake.status")

type recorder struct {
	status  int
	body    any
	failure *Failure
}

type fakeCodec struct {
	panicOn    string // "decode", "encode", "encodeError"
	encodeErr  error
	encErrFail error
}

func (c fakeCodec) Decode(_ context.Context, _ Route, req string, in any) error {
	if c.panicOn == "decode" {
		panic("codec decode exploded")
	}
	return json.Unmarshal([]byte(req), in)
}

func (c fakeCodec) Encode(_ context.Context, r Route, resp *recorder, out any) error {
	if c.panicOn == "encode" {
		panic("codec encode exploded")
	}
	if c.encodeErr != nil {
		return c.encodeErr
	}
	resp.status, resp.body = MetaOr(r, successStatus, 200), out
	return nil
}

func (c fakeCodec) EncodeError(_ context.Context, _ Route, resp *recorder, f Failure) error {
	if c.panicOn == "encodeError" {
		panic("codec encodeError exploded")
	}
	if c.encErrFail != nil {
		return c.encErrFail
	}
	resp.status, resp.failure = 400+int(f.Kind), &f
	return nil
}

type failures struct {
	mu   sync.Mutex
	list []Failure
}

func (f *failures) add(_ context.Context, fl Failure) {
	f.mu.Lock()
	f.list = append(f.list, fl)
	f.mu.Unlock()
}

func createRoute(opts ...RouteOption) Route {
	return NewRoute("orders.create", Bind(create), opts...)
}

func serve(t *testing.T, codec fakeCodec, r Route, req string, opts ...PipelineOption) (*recorder, *failures, error) {
	t.Helper()
	fs := &failures{}
	p := NewPipeline[string, *recorder](codec, append([]PipelineOption{OnFailure(fs.add)}, opts...)...)
	rec := &recorder{}
	err := p.Serve(context.Background(), r, req, rec)
	return rec, fs, err
}

func TestServeSuccessUsesRouteMeta(t *testing.T) {
	rec, fs, err := serve(t, fakeCodec{}, createRoute(WithMeta(successStatus, 201)), `{"Item":"book","Qty":2}`)
	if err != nil || rec.status != 201 || rec.body.(*createResult).ID != "order-book" || len(fs.list) != 0 {
		t.Fatalf("err=%v rec=%+v failures=%v", err, rec, fs.list)
	}

	rec, _, _ = serve(t, fakeCodec{}, createRoute(), `{"Item":"book","Qty":2}`)
	if rec.status != 200 {
		t.Fatalf("default status = %d", rec.status)
	}
}

func TestServeFailures(t *testing.T) {
	for _, tc := range []struct {
		name, req string
		stage     Stage
		kind      Kind
		msg       string
	}{
		{"broken input", `{"Item":`, StageDecode, KindInvalid, "unexpected end of JSON input"},
		{"validation", `{"Item":"book","Qty":0}`, StageHandle, KindInvalid, "qty must be > 0, got 0"},
		{"business error", `{"Item":"missing","Qty":1}`, StageHandle, KindNotFound, `item "missing" not found`},
		{"internal masked", `{"Item":"db","Qty":1}`, StageHandle, KindInternal, "internal error"},
		{"handler panic", `{"Item":"boom","Qty":1}`, StageHandle, KindInternal, "internal error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, fs, err := serve(t, fakeCodec{}, createRoute(), tc.req)
			if err == nil || rec.failure == nil {
				t.Fatalf("expected failure, rec=%+v", rec)
			}
			f := *rec.failure
			if f.Stage != tc.stage || f.Kind != tc.kind || f.Message != tc.msg || f.Route != "orders.create" {
				t.Fatalf("failure = %+v", f)
			}
			if f.Kind == KindInternal && strings.Contains(f.Message, "connection reset") {
				t.Fatal("internal message leaked to the client")
			}
			if len(fs.list) != 1 || fs.list[0].Err == nil {
				t.Fatalf("OnFailure got %v", fs.list)
			}
		})
	}
}

func TestInternalMessageOption(t *testing.T) {
	rec, fs, _ := serve(t, fakeCodec{}, createRoute(), `{"Item":"db","Qty":1}`, InternalMessage("try later"))
	if rec.failure.Message != "try later" || !strings.Contains(fs.list[0].Err.Error(), "connection reset") {
		t.Fatalf("client=%q log=%v", rec.failure.Message, fs.list[0].Err)
	}
}

func TestCodecPanicsAreContained(t *testing.T) {
	// decode panic → Internal, answered with an error
	rec, _, err := serve(t, fakeCodec{panicOn: "decode"}, createRoute(), `{}`)
	if !errors.Is(err, ErrPanic) || rec.failure == nil || rec.failure.Kind != KindInternal || rec.failure.Stage != StageDecode {
		t.Fatalf("decode panic: err=%v rec=%+v", err, rec)
	}

	// encode panic → reported, no error written over a half-written answer
	rec, fs, err := serve(t, fakeCodec{panicOn: "encode"}, createRoute(), `{"Item":"x","Qty":1}`)
	if !errors.Is(err, ErrPanic) || rec.failure != nil || len(fs.list) != 1 || fs.list[0].Stage != StageEncode {
		t.Fatalf("encode panic: err=%v rec=%+v failures=%v", err, rec, fs.list)
	}

	// encodeError panic → reported as a second failure
	_, fs, _ = serve(t, fakeCodec{panicOn: "encodeError"}, createRoute(), `{"Item":"missing","Qty":1}`)
	if len(fs.list) != 2 || fs.list[0].Kind != KindNotFound || fs.list[1].Stage != StageEncode {
		t.Fatalf("failures = %+v", fs.list)
	}
}

func TestEncodeErrorsReported(t *testing.T) {
	gone := errors.New("client went away")
	_, fs, err := serve(t, fakeCodec{encodeErr: gone}, createRoute(), `{"Item":"x","Qty":1}`)
	if !errors.Is(err, gone) || len(fs.list) != 1 || fs.list[0].Stage != StageEncode {
		t.Fatalf("err=%v failures=%v", err, fs.list)
	}

	_, fs, _ = serve(t, fakeCodec{encErrFail: gone}, createRoute(), `{"Item":"missing","Qty":1}`)
	if len(fs.list) != 2 || !errors.Is(fs.list[1].Err, gone) {
		t.Fatalf("failures = %+v", fs.list)
	}
}

func TestObserveAndBrokenHooks(t *testing.T) {
	var seen []string
	observe := Observe(func(_ context.Context, r Route, err error, took time.Duration) {
		seen = append(seen, r.Name()+":"+KindOf(errOrOK(err)).String())
		if took < 0 {
			t.Error("negative duration")
		}
	})
	_, _, _ = serve(t, fakeCodec{}, createRoute(), `{"Item":"x","Qty":1}`, observe)
	_, _, _ = serve(t, fakeCodec{}, createRoute(), `{"Item":"missing","Qty":1}`, observe)
	if strings.Join(seen, " ") != "orders.create:conflict orders.create:not_found" {
		t.Fatalf("observed %v", seen)
	}

	// A panicking logger or metric must not break serving.
	p := NewPipeline[string, *recorder](fakeCodec{},
		OnFailure(func(context.Context, Failure) { panic("logger down") }),
		Observe(func(context.Context, Route, error, time.Duration) { panic("metrics down") }))
	rec := &recorder{}
	if err := p.Serve(context.Background(), createRoute(), `{"Item":"missing","Qty":1}`, rec); KindOf(err) != KindNotFound || rec.failure == nil {
		t.Fatalf("err=%v rec=%+v", err, rec)
	}
}

// errOrOK maps success to a marker kind so the test can print it.
func errOrOK(err error) error {
	if err == nil {
		return Conflict("ok")
	}
	return err
}

func TestRouteMeta(t *testing.T) {
	retries := NewMetaKey[int]("queue.retries")
	other := NewMetaKey[int]("queue.retries") // same name, different key
	r := createRoute(WithMeta(retries, 5))
	if v, ok := Meta(r, retries); !ok || v != 5 {
		t.Fatalf("Meta = %v %v", v, ok)
	}
	if _, ok := Meta(r, other); ok {
		t.Fatal("keys with the same name must not clash")
	}
	if MetaOr(r, other, 3) != 3 || r.Name() != "orders.create" || retries.String() != "queue.retries" {
		t.Fatal("MetaOr / Name / String")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("NewRoute with a zero Action must panic")
		}
	}()
	NewRoute("x", Action{})
}
