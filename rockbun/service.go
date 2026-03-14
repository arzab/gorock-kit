package rockbun

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type service struct {
	cfg Configs
	db  *bun.DB
}

// NewService creates a Service that is ready to be initialised.
// No connection is opened — call Init to establish the connection pool.
func NewService(cfg Configs) Service {
	return &service{cfg: cfg}
}

// Init opens the connection pool and verifies connectivity via Ping.
func (s *service) Init(ctx context.Context) error {
	dsn := s.cfg.DSN
	if dsn == "" {
		dsn = buildDSN(s.cfg)
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	sqldb.SetMaxOpenConns(intOrDefault(s.cfg.MaxOpenConns, 25))
	sqldb.SetMaxIdleConns(intOrDefault(s.cfg.MaxIdleConns, 5))
	sqldb.SetConnMaxLifetime(durOrDefault(s.cfg.ConnMaxLifetime, time.Hour))
	sqldb.SetConnMaxIdleTime(durOrDefault(s.cfg.ConnMaxIdleTime, 30*time.Minute))

	db := bun.NewDB(sqldb, pgdialect.New())
	for _, hook := range s.cfg.QueryHooks {
		db = db.WithQueryHook(hook)
	}
	s.db = db

	return s.Ping(ctx)
}

// Stop closes the connection pool.
func (s *service) Stop() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *service) DB() *bun.DB {
	return s.db
}

func (s *service) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *service) RunInTx(ctx context.Context, opts *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error) error {
	return s.db.RunInTx(ctx, opts, fn)
}

func buildDSN(cfg Configs) string {
	host := strOrDefault(cfg.Host, "localhost")
	port := intOrDefault(cfg.Port, 5432)
	sslMode := strOrDefault(cfg.SSLMode, "disable")
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password, host, port, cfg.Database, sslMode,
	)
}

func intOrDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func durOrDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func strOrDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
