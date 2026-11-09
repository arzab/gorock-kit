package rockredis

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// A failed Init must not leave an open client behind, and Stop must be safe after it.
func TestInitFailureLeavesNoClient(t *testing.T) {
	s := NewService(Configs{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond}).(*service)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Init(ctx); err == nil {
		t.Fatal("expected Init to fail against a closed port")
	}
	if s.client != nil {
		t.Fatal("client must stay nil after a failed Init")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop after failed Init: %v", err)
	}
}

func TestStopIdempotent(t *testing.T) {
	s := NewService(Configs{}).(*service)
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestClientAccessor(t *testing.T) {
	if Client(NewService(Configs{})) != nil {
		t.Fatal("Client must be nil before Init")
	}
	if Client(nil) != nil {
		t.Fatal("Client(nil) must be nil")
	}
	s := NewService(Configs{}).(*service)
	s.client = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer func() { _ = s.client.Close() }()
	if Client(s) != s.client {
		t.Fatal("Client must return the underlying client")
	}
}
