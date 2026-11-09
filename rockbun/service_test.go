package rockbun

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestBuildDSNEscapesCredentials(t *testing.T) {
	cfg := Configs{User: "app@corp", Password: "p@ss:w/rd#1?", Host: "db.local", Port: 6432, Database: "main"}
	dsn := buildDSN(cfg)

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN %q does not parse: %v", dsn, err)
	}
	pass, _ := u.User.Password()
	if u.User.Username() != cfg.User || pass != cfg.Password {
		t.Fatalf("credentials round-trip failed: user=%q pass=%q (dsn %q)", u.User.Username(), pass, dsn)
	}
	if u.Host != "db.local:6432" || u.Path != "/main" || u.Query().Get("sslmode") != "disable" {
		t.Fatalf("unexpected DSN %q", dsn)
	}
}

func TestBuildDSNDefaults(t *testing.T) {
	got := buildDSN(Configs{Database: "db"})
	if want := "postgres://localhost:5432/db?sslmode=disable"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildDSNIPv6Host(t *testing.T) {
	u, err := url.Parse(buildDSN(Configs{Host: "::1", Database: "db"}))
	if err != nil || u.Hostname() != "::1" || u.Port() != "5432" {
		t.Fatalf("u=%v err=%v", u, err)
	}
}

// A failed Init must not leave an open pool behind, and Stop must be safe after it.
func TestInitFailureLeavesNoPool(t *testing.T) {
	s := NewService(Configs{DSN: "postgres://u:p@127.0.0.1:1/db?sslmode=disable"}).(*service)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Init(ctx); err == nil {
		t.Fatal("expected Init to fail against a closed port")
	}
	if s.DB() != nil {
		t.Fatal("DB() must be nil after a failed Init")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop after failed Init: %v", err)
	}
}
