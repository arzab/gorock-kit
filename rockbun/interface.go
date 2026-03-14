package rockbun

import (
	"context"
	"database/sql"

	"github.com/uptrace/bun"
)

// Service wraps a bun.DB and provides lifecycle management.
//
// Call order: Init → use → Stop.
//
// Use DB() to access the full bun query builder for selects, inserts,
// updates, deletes, and schema migrations.
// Use RunInTx for transactional work.
type Service interface {
	// Init opens the connection pool and verifies connectivity via Ping.
	Init(ctx context.Context) error

	// Stop closes the connection pool.
	Stop() error

	// DB returns the underlying *bun.DB for building queries.
	// Must not be called before Init.
	DB() *bun.DB

	// Ping verifies the database connection is alive.
	Ping(ctx context.Context) error

	// RunInTx runs fn inside a database transaction.
	// The transaction is committed if fn returns nil, rolled back otherwise.
	// Pass nil opts to use the database default isolation level.
	RunInTx(ctx context.Context, opts *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error) error
}
