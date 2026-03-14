# rockbun

A [bun](https://bun.uptrace.dev/) ORM wrapper for PostgreSQL that integrates with `rockconfig` and provides lifecycle management (`Ping`, `Close`, `RunInTx`) via the `Service` interface.

Unlike `rockredis`, this package does **not** wrap individual operations — bun's query builder is rich enough to use directly. Instead `rockbun` handles connection setup, pool configuration, and transactions.

## Quick Start

```go
svc := rockbun.NewService(rockbun.Configs{
    DSN: "postgres://user:pass@localhost:5432/mydb?sslmode=disable",
})

ctx := context.Background()
if err := svc.Ping(ctx); err != nil {
    log.Fatal(err)
}
defer svc.Close()

db := svc.DB() // *bun.DB — use for all queries
```

## Loading Config from File

`rockbun.Configs` is compatible with `rockconfig.InitFromFile`. All fields are optional.

```yaml
# config.yaml — option 1: DSN
dsn: "postgres://user:pass@localhost:5432/mydb?sslmode=disable"

# option 2: individual fields (used when dsn is empty)
host: "localhost"
port: 5432
user: "myuser"
password: "secret"
database: "mydb"
ssl_mode: "disable"

# pool settings
max_open_conns: 25
max_idle_conns: 5
conn_max_lifetime: "1h"
conn_max_idle_time: "30m"
```

```go
cfg, err := rockconfig.InitFromFile[rockbun.Configs]("config.yaml")
if err != nil {
    log.Fatal(err)
}

// QueryHooks must be set in code
cfg.QueryHooks = []bun.QueryHook{bundebug.NewQueryHook()}

svc := rockbun.NewService(*cfg)
```

## Config

```go
type Configs struct {
    DSN string  // "postgres://user:pass@host:port/db?sslmode=disable"
                // takes priority over individual fields

    Host     string // default "localhost"
    Port     int    // default 5432
    User     string
    Password string
    Database string
    SSLMode  string // default "disable"

    MaxOpenConns    int           // default 25
    MaxIdleConns    int           // default 5
    ConnMaxLifetime time.Duration // default 1h
    ConnMaxIdleTime time.Duration // default 30m

    QueryHooks []bun.QueryHook `config:"-"` // set in code only
}
```

## Queries

Access the full bun query builder via `DB()`:

```go
db := svc.DB()

// Select
var users []User
err := db.NewSelect().Model(&users).Where("active = ?", true).Scan(ctx)

// Insert
user := &User{Name: "Alice", Email: "alice@example.com"}
_, err = db.NewInsert().Model(user).Exec(ctx)

// Update
_, err = db.NewUpdate().Model(user).Column("name").Where("id = ?", user.ID).Exec(ctx)

// Delete
_, err = db.NewDelete().Model(user).Where("id = ?", user.ID).Exec(ctx)

// Raw SQL
var count int
err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
```

## Transactions

```go
err := svc.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
    if _, err := tx.NewInsert().Model(&order).Exec(ctx); err != nil {
        return err
    }
    if _, err := tx.NewUpdate().Model(&stock).
        Set("qty = qty - ?", order.Qty).
        Where("id = ?", stock.ID).
        Exec(ctx); err != nil {
        return err
    }
    return nil
})
```

Pass `nil` opts for the default isolation level, or set it explicitly:

```go
svc.RunInTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, fn)
```

## Query Logging

Use bun's built-in `bundebug` hook to log all queries:

```go
import "github.com/uptrace/bun/extra/bundebug"

cfg.QueryHooks = []bun.QueryHook{
    bundebug.NewQueryHook(bundebug.WithVerbose(true)),
}
```

Or implement `bun.QueryHook` for a custom logger:

```go
type myHook struct{}

func (h *myHook) BeforeQuery(ctx context.Context, e *bun.QueryEvent) context.Context {
    return ctx
}

func (h *myHook) AfterQuery(ctx context.Context, e *bun.QueryEvent) {
    log.Info("query",
        rocklog.Str("query", e.Query),
        rocklog.Dur("duration", time.Since(e.StartTime)),
        rocklog.Err(e.Err),
    )
}

cfg.QueryHooks = []bun.QueryHook{&myHook{}}
```

## Schema Migrations

bun's migration system works directly on `*bun.DB`:

```go
db := svc.DB()
migrator := migrate.NewMigrator(db, migrations.Migrations)
if err := migrator.Init(ctx); err != nil {
    log.Fatal(err)
}
if err := migrator.Migrate(ctx); err != nil {
    log.Fatal(err)
}
```

See the [bun migrations guide](https://bun.uptrace.dev/guide/migrations.html) for details.

## Limitations

- PostgreSQL only (via `pgdriver`). For MySQL or SQLite, use `bun.NewDB` directly with the appropriate driver and pass the `*bun.DB` through a custom wrapper.
- `QueryHooks` are applied at construction time; they cannot be added or removed after `NewService`.
