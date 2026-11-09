package rockfiber

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

const lifecycleTimeout = 5 * time.Second

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return strconv.Itoa(port)
}

func waitListening(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(lifecycleTimeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not start listening on %s", port)
}

// waitPortFree waits until nothing accepts connections on the port.
// A dial check is used rather than re-binding: on Windows a dual-stack
// listener can bind next to fiber's IPv4 listener and give a false "free".
func waitPortFree(t *testing.T, port string) {
	t.Helper()
	deadline := time.Now().Add(lifecycleTimeout)
	for {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 50*time.Millisecond)
		if err != nil {
			return
		}
		_ = c.Close()
		if time.Now().After(deadline) {
			t.Fatalf("server still accepting connections on %s after shutdown", port)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startExec(t *testing.T, a *FiberApp, ctx context.Context) <-chan error {
	t.Helper()
	if err := a.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	res := make(chan error, 1)
	go func() { res <- a.Exec(ctx) }()
	return res
}

func waitExecDone(t *testing.T, res <-chan error) {
	t.Helper()
	select {
	case err := <-res:
		if err != nil {
			t.Fatalf("Exec: %v", err)
		}
	case <-time.After(lifecycleTimeout):
		t.Fatal("Exec did not return")
	}
}

func quietConfig(port string) Config {
	return Config{Port: port, App: fiber.Config{DisableStartupMessage: true}, ShutdownTimeout: time.Second}
}

// Regression: stopOnce was never reset, so after the engine's restart
// sequence (Stop → Init → Exec) the new server was never shut down.
func TestRestartShutsDownNewServer(t *testing.T) {
	port := freePort(t)
	a := New(quietConfig(port))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res := startExec(t, a, ctx)
	waitListening(t, port)
	if errs := a.Stop(); len(errs) > 0 {
		t.Fatalf("Stop: %v", errs)
	}
	waitExecDone(t, res)
	waitPortFree(t, port)

	// Restart, then shut down the way the engine does: cancel + Stop.
	res = startExec(t, a, ctx)
	waitListening(t, port)
	cancel()
	if errs := a.Stop(); len(errs) > 0 {
		t.Fatalf("Stop after restart: %v", errs)
	}
	waitExecDone(t, res)
	waitPortFree(t, port)
}

func TestExecRequiresInitEachTime(t *testing.T) {
	a := New(quietConfig(freePort(t)))
	if err := a.Exec(context.Background()); err == nil {
		t.Fatal("Exec before Init must fail")
	}
}

func TestServerWithoutEndpoints(t *testing.T) {
	port := freePort(t)
	a := New(quietConfig(port)) // no endpoints: /status only
	ctx, cancel := context.WithCancel(context.Background())
	res := startExec(t, a, ctx)
	waitListening(t, port)
	cancel()
	a.Stop()
	waitExecDone(t, res)
}

func TestStopBeforeInit(t *testing.T) {
	a := New(quietConfig(freePort(t)))
	if errs := a.Stop(); len(errs) > 0 {
		t.Fatalf("Stop: %v", errs)
	}
}
