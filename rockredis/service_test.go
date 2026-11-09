package rockredis

import (
	"context"
	"testing"
	"time"
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
