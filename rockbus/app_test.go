package rockbus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 3 * time.Second

type errCollector struct {
	mu   sync.Mutex
	errs []error
}

func (c *errCollector) onError(_ context.Context, _ Event, err error) {
	c.mu.Lock()
	c.errs = append(c.errs, err)
	c.mu.Unlock()
}

func (c *errCollector) count(target error) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, err := range c.errs {
		if errors.Is(err, target) {
			n++
		}
	}
	return n
}

func (c *errCollector) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.errs)
}

func startBus(t *testing.T, a *App, ctx context.Context) <-chan error {
	t.Helper()
	if err := a.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	res := make(chan error, 1)
	go func() { res <- a.Exec(ctx) }()
	return res
}

func waitDone(t *testing.T, res <-chan error) {
	t.Helper()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("Exec: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Exec did not return")
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// Regression: PublishAsync racing with Stop could enqueue an event after the
// worker had drained and exited — lost with no OnError. Now every event is
// either handled or reported.
func TestNoSilentLossDuringStop(t *testing.T) {
	for iter := 0; iter < 20; iter++ {
		var handled atomic.Int64
		errs := &errCollector{}
		a := NewApp(Config{QueueSize: 64}, errs.onError,
			On("t", func(context.Context) error { handled.Add(1); return nil }),
		)
		res := startBus(t, a, context.Background())

		const publishers, perPublisher = 50, 40
		var wg sync.WaitGroup
		start := make(chan struct{})
		for p := 0; p < publishers; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < perPublisher; i++ {
					a.PublishAsync(context.Background(), Event{Topic: "t"})
				}
			}()
		}
		close(start)
		time.Sleep(time.Duration(iter%5) * 100 * time.Microsecond)
		a.Stop()
		wg.Wait()
		waitDone(t, res)

		got := handled.Load() + int64(errs.total())
		if want := int64(publishers * perPublisher); got != want {
			t.Fatalf("iteration %d: handled %d + reported %d = %d, want %d",
				iter, handled.Load(), errs.total(), got, want)
		}
	}
}

// Regression: queues were created in Exec, but the engine runs every app's
// Init before any Exec and then starts Execs concurrently — an app could
// publish before the bus had queues and get ErrNoWorker.
func TestPublishBetweenInitAndExecIsDelivered(t *testing.T) {
	delivered := make(chan struct{}, 1)
	errs := &errCollector{}
	a := NewApp(Config{}, errs.onError,
		On("t", func(context.Context) error { delivered <- struct{}{}; return nil }),
	)
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.PublishAsync(context.Background(), Event{Topic: "t"})

	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- a.Exec(ctx) }()

	select {
	case <-delivered:
	case <-time.After(testTimeout):
		t.Fatalf("event published before Exec was not delivered; errors: %v", errs.errs)
	}
	cancel()
	waitDone(t, res)
}

func TestStopBeforeExecReportsQueuedEvents(t *testing.T) {
	errs := &errCollector{}
	a := NewApp(Config{}, errs.onError, On("t", func(context.Context) error { return nil }))
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		a.PublishAsync(context.Background(), Event{Topic: "t"})
	}
	a.Stop()
	if got := errs.count(ErrAppStopped); got != 3 {
		t.Fatalf("ErrAppStopped reported %d times, want 3", got)
	}
}

func TestPublishAfterStop(t *testing.T) {
	errs := &errCollector{}
	a := NewApp(Config{}, errs.onError, On("t", func(context.Context) error { return nil }))
	res := startBus(t, a, context.Background())
	a.Stop()
	waitDone(t, res)
	a.PublishAsync(context.Background(), Event{Topic: "t"})
	if errs.count(ErrAppStopped) != 1 {
		t.Fatalf("errors: %v", errs.errs)
	}
}

func TestPublishBeforeInit(t *testing.T) {
	errs := &errCollector{}
	a := NewApp(Config{}, errs.onError, On("t", func(context.Context) error { return nil }))
	a.PublishAsync(context.Background(), Event{Topic: "t"})
	if errs.total() != 1 {
		t.Fatalf("errors: %v", errs.errs)
	}
}

func TestRestart(t *testing.T) {
	var handled atomic.Int64
	a := NewApp(Config{}, nil, On("t", func(context.Context) error { handled.Add(1); return nil }))

	ctx, cancel := context.WithCancel(context.Background())
	res := startBus(t, a, ctx)
	a.PublishAsync(context.Background(), Event{Topic: "t"})
	eventually(t, "first event", func() bool { return handled.Load() == 1 })
	a.Stop()
	waitDone(t, res)

	res = startBus(t, a, ctx)
	a.PublishAsync(context.Background(), Event{Topic: "t"})
	eventually(t, "event after restart", func() bool { return handled.Load() == 2 })
	cancel()
	a.Stop() // engine calls both; must not panic on double close
	waitDone(t, res)
}

func TestStopIsIdempotent(t *testing.T) {
	a := NewApp(Config{}, nil, On("t", func(context.Context) error { return nil }))
	a.Stop() // before Init
	res := startBus(t, a, context.Background())
	a.Stop()
	a.Stop()
	waitDone(t, res)
}

func TestQueueFull(t *testing.T) {
	block := make(chan struct{})
	errs := &errCollector{}
	a := NewApp(Config{QueueSize: 1}, errs.onError,
		On("t", func(context.Context) error { <-block; return nil }),
	)
	res := startBus(t, a, context.Background())
	// One event is picked up by the worker (blocked), one fills the queue,
	// the rest overflow. Publish until we see an overflow.
	eventually(t, "ErrQueueFull", func() bool {
		a.PublishAsync(context.Background(), Event{Topic: "t"})
		return errs.count(ErrQueueFull) > 0
	})
	close(block)
	a.Stop()
	waitDone(t, res)
}

func TestUnknownTopic(t *testing.T) {
	errs := &errCollector{}
	a := NewApp(Config{}, errs.onError, On("t", func(context.Context) error { return nil }))
	res := startBus(t, a, context.Background())
	a.PublishAsync(context.Background(), Event{Topic: "other"})
	if errs.count(ErrNoWorker) != 1 {
		t.Fatalf("errors: %v", errs.errs)
	}
	a.Stop()
	waitDone(t, res)
}

func TestOrderWithinTopic(t *testing.T) {
	var mu sync.Mutex
	var got []int
	a := NewApp(Config{}, nil, On("t", func(ctx context.Context) error {
		n, err := Payload[int](ctx)
		if err != nil {
			return err
		}
		mu.Lock()
		got = append(got, n)
		mu.Unlock()
		return nil
	}))
	res := startBus(t, a, context.Background())
	const n = 500
	for i := 0; i < n; i++ {
		a.PublishAsync(context.Background(), Event{Topic: "t", Payload: i})
	}
	a.Stop() // drains before Exec returns
	waitDone(t, res)
	if len(got) != n {
		t.Fatalf("got %d events, want %d", len(got), n)
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("event %d out of order: got payload %d", i, v)
		}
	}
}

func TestHandlerPanicReported(t *testing.T) {
	errs := &errCollector{}
	a := NewApp(Config{}, errs.onError, On("t", func(context.Context) error { panic("boom") }))
	res := startBus(t, a, context.Background())
	a.PublishAsync(context.Background(), Event{Topic: "t"})
	eventually(t, "ErrPanic", func() bool { return errs.count(ErrPanic) == 1 })
	a.Stop()
	waitDone(t, res)
}

func TestOnErrorMayPublish(t *testing.T) {
	// OnError runs outside internal locks, so publishing from it must not deadlock.
	var a *App
	var calls atomic.Int32
	a = NewApp(Config{}, func(ctx context.Context, e Event, err error) {
		if calls.Add(1) == 1 {
			a.PublishAsync(ctx, Event{Topic: "t"})
		}
	}, On("t", func(context.Context) error { return nil }))
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.PublishAsync(context.Background(), Event{Topic: "t"})
	done := make(chan struct{})
	go func() { a.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Stop deadlocked when OnError published")
	}
}

func TestSubscribeAfterInit(t *testing.T) {
	a := NewApp(Config{}, nil, On("known", func(context.Context) error { return nil }))
	if err := a.Subscribe("before", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Subscribe before Init: %v", err)
	}
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Subscribe("known", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("extra handler for existing topic: %v", err)
	}
	if err := a.Subscribe("brand-new", func(context.Context) error { return nil }); !errors.Is(err, ErrNoWorker) {
		t.Fatalf("new topic after Init: err = %v", err)
	}
	if err := a.Subscribe("known", nil); err == nil {
		t.Fatal("nil handler accepted")
	}

	// After Stop, a new topic is accepted again and gets a queue on the next Init.
	a.Stop()
	if err := a.Subscribe("brand-new", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Subscribe after Stop: %v", err)
	}
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.enqueue(context.Background(), Event{Topic: "brand-new"}); err != nil {
		t.Fatalf("topic subscribed between Stop and Init has no queue: %v", err)
	}
	a.Stop()
}

func TestPublishSyncJoinsErrors(t *testing.T) {
	e1, e2 := errors.New("one"), errors.New("two")
	a := NewApp(Config{}, nil,
		On("t", func(context.Context) error { return e1 }),
		On("t", func(context.Context) error { return e2 }),
	)
	err := a.Publish(context.Background(), Event{Topic: "t"})
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelledPublisherContextIsDetached(t *testing.T) {
	seen := make(chan error, 1)
	a := NewApp(Config{}, nil, On("t", func(ctx context.Context) error { seen <- ctx.Err(); return nil }))
	res := startBus(t, a, context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.PublishAsync(ctx, Event{Topic: "t"})
	select {
	case err := <-seen:
		if err != nil {
			t.Fatalf("handler ctx should be detached from publisher, got %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("event not delivered")
	}
	a.Stop()
	waitDone(t, res)
}
