package rockbun_test

import (
	"context"
	"database/sql"
	"log"

	"github.com/arzab/gorock-kit/rockbun"
	"github.com/uptrace/bun"
)

// Compiled, not run: mirrors the README Quick Start.

func ExampleNewService() {
	svc := rockbun.NewService(rockbun.Configs{
		Host: "localhost", User: "app", Password: "p@ss:w/rd", Database: "app", SSLMode: "verify-full",
	})

	ctx := context.Background()
	if err := svc.Init(ctx); err != nil { // opens the pool and pings
		log.Fatal(err)
	}
	defer func() { _ = svc.Stop() }()

	var n int
	if err := svc.DB().NewSelect().ColumnExpr("1").Scan(ctx, &n); err != nil {
		log.Fatal(err)
	}
	err := svc.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewRaw("SELECT 1").Exec(ctx)
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
}
