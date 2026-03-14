package rockbun

import (
	"time"

	"github.com/uptrace/bun"
)

// Configs holds configuration for a bun DB connection (PostgreSQL via pgdriver).
//
// Compatible with rockconfig.InitFromFile.
// If DSN is set it takes priority; otherwise the connection string is built
// from the individual Host/Port/User/Password/Database/SSLMode fields.
//
// Example config.yaml:
//
//	dsn: "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
//
// — or individual fields —
//
//	host: "localhost"
//	port: 5432
//	user: "myuser"
//	password: "secret"
//	database: "mydb"
//	ssl_mode: "disable"
//	max_open_conns: 25
//	max_idle_conns: 5
//	conn_max_lifetime: "1h"
//	conn_max_idle_time: "30m"
type Configs struct {
	// DSN takes priority over individual fields when non-empty.
	// Format: "postgres://user:pass@host:port/db?sslmode=disable"
	DSN string `config:",omitempty"`

	// Individual connection fields — used when DSN is empty.
	Host     string `config:",omitempty"` // default "localhost"
	Port     int    `config:",omitempty"` // default 5432
	User     string `config:",omitempty"`
	Password string `config:",omitempty"`
	Database string `config:",omitempty"`
	SSLMode  string `config:",omitempty"` // default "disable"

	// Connection pool settings.
	MaxOpenConns    int           `config:",omitempty"` // default 25
	MaxIdleConns    int           `config:",omitempty"` // default 5
	ConnMaxLifetime time.Duration `config:",omitempty"` // default 1h
	ConnMaxIdleTime time.Duration `config:",omitempty"` // default 30m

	// QueryHooks are registered on the bun.DB instance.
	// Use them to add query logging, tracing, or metrics.
	// Must be set in code — function types are not loadable from file.
	QueryHooks []bun.QueryHook `config:"-"`
}
