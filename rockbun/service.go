package rockbun

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type service struct {
	cfg    Configs
	db     *bun.DB
	closed bool // Stop closed db; it is kept so calls fail with "sql: database is closed" instead of panicking
}

// NewService creates a Service that is ready to be initialised.
// No connection is opened — call Init to establish the connection pool.
func NewService(cfg Configs) Service {
	return &service{cfg: cfg}
}

// Init opens the connection pool and verifies connectivity via Ping.
// If Ping fails, the new pool is closed and DB() stays nil.
// Calling Init again closes the previous pool first.
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

	if err := db.PingContext(ctx); err != nil {
		return errors.Join(err, db.Close())
	}

	if prev := s.db; prev != nil && !s.closed {
		_ = prev.Close()
	}
	s.db = db
	s.closed = false
	return nil
}

// Stop closes the connection pool. Safe to call multiple times.
func (s *service) Stop() error {
	if s.db == nil || s.closed {
		return nil
	}
	s.closed = true
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

// buildDSN assembles a postgres URL from individual fields. User, password
// and database are escaped, so values containing '@', ':', '/' or '#' are safe.
func buildDSN(cfg Configs) string {
	host := strOrDefault(cfg.Host, "localhost")
	port := intOrDefault(cfg.Port, 5432)
	sslMode := strOrDefault(cfg.SSLMode, "disable")

	u := url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + cfg.Database,
		RawQuery: url.Values{"sslmode": {sslMode}}.Encode(),
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	return u.String()
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
