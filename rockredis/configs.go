package rockredis

import "time"

// Configs holds configuration for a Redis connection.
//
// Compatible with rockconfig.InitFromFile — all fields are optional (omitempty).
//
// Example config.yaml:
//
//	addr: "localhost:6379"
//	password: "secret"
//	db: 0
//	dial_timeout: "5s"
//	read_timeout: "3s"
//	write_timeout: "3s"
//	pool_size: 10
//	min_idle_conns: 2
//	tls_enabled: false
type Configs struct {
	Addr     string `config:",omitempty"` // host:port; default "localhost:6379"
	Password string `config:",omitempty"`
	DB       int    `config:",omitempty"` // Redis database index (0–15)

	DialTimeout  time.Duration `config:",omitempty"` // default 5s
	ReadTimeout  time.Duration `config:",omitempty"` // default 3s
	WriteTimeout time.Duration `config:",omitempty"` // default 3s

	PoolSize     int `config:",omitempty"` // connection pool size; default 10
	MinIdleConns int `config:",omitempty"` // minimum idle connections

	TLSEnabled bool `config:",omitempty"` // enable TLS (uses system cert pool)
}
