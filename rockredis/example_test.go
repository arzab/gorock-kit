package rockredis_test

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/arzab/gorock-kit/rockredis"
)

// Compiled, not run: mirrors the README Quick Start.

func ExampleNewService() {
	svc := rockredis.NewService(rockredis.Configs{Addr: "localhost:6379", Password: "secret"})

	ctx := context.Background()
	if err := svc.Init(ctx); err != nil { // creates the client and pings
		log.Fatal(err)
	}
	defer func() { _ = svc.Stop() }()

	if err := svc.Set(ctx, "k", "v", time.Minute); err != nil {
		log.Fatal(err)
	}
	if _, err := svc.Get(ctx, "missing"); errors.Is(err, rockredis.ErrNil) {
		log.Println("not found")
	}
	ok, err := svc.SetNX(ctx, "lock", "1", 10*time.Second)
	log.Println(ok, err)

	// Commands not covered by Service:
	rank, err := rockredis.Client(svc).ZRevRank(ctx, "leaderboard", "alice").Result()
	log.Println(rank, err)
}
