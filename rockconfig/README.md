# rockconfig

Config file loader for Go with automatic snake_case mapping, env variable expansion, and struct validation.

## Overview

`rockconfig` reads a config file (JSON or YAML), maps its values into a typed Go struct, and validates that all required fields are present. No `json` or `yaml` struct tags needed — field names are automatically converted to `snake_case`.

## Quick Start

```go
type Config struct {
    Host string
    Port int
    Debug bool `config:",omitempty"`
}

cfg, err := rockconfig.InitFromFile[Config]("config.yaml")
if err != nil {
    log.Fatal(err)
}
```

```yaml
# config.yaml
host: localhost
port: 8080
```

## Field Mapping

By default, struct field names are converted to `snake_case` automatically:

| Go field     | Config key    |
|--------------|---------------|
| `Host`       | `host`        |
| `Port`       | `port`        |
| `MyField`    | `my_field`    |
| `HTTPServer` | `http_server` |
| `UserID`     | `user_id`     |

## Config Tag

Use the `config` tag to customise field behaviour, similar to `bun` or `encoding/json`:

```
config:"[name][,option[,option...]]"
```

| Tag                       | Behaviour                                              |
|---------------------------|--------------------------------------------------------|
| _(no tag)_                | snake_case name, required                              |
| `config:"-"`              | ignore this field entirely                             |
| `config:"host"`           | explicit key name `"host"`, required                   |
| `config:",omitempty"`     | snake_case name, field is optional                     |
| `config:",default=8080"`  | snake_case name, use `8080` if key is absent           |
| `config:",env=PORT"`      | snake_case name, read value from `$PORT` env var       |

Options can be combined (see ordering note below):

```go
type Config struct {
    Host    string        // required, mapped as "host"
    Port    int           `config:",default=8080"`       // optional with default
    Debug   bool          `config:",omitempty"`           // optional, no default
    Secret  string        `config:",env=APP_SECRET"`      // read from $APP_SECRET
    Timeout time.Duration `config:",default=30s"`         // supports duration strings
    Token   string        `config:"-"`                   // ignored
}
```

> **Note:** `default=` must be the **last** option in the tag because it consumes everything after `=` as the value (this allows commas inside default values, e.g. `default=a,b,c`).

## Validation

After loading, all fields without `omitempty`, `default=`, or `-` are checked for emptiness:

| Type              | Considered empty       |
|-------------------|------------------------|
| `string`          | `""`                   |
| `int` / `uint`    | `0`                    |
| `float`           | `0.0`                  |
| `slice` / `map`   | `nil` or length 0      |
| `array`           | length 0               |
| pointer           | `nil`                  |
| `bool`            | **never** (see below)  |

If any required fields are missing, `InitFromFile` returns an error listing all of them:

```
rockconfig: missing required fields:
  Config.Host
  Config.Database.Password
```

### Bool fields

`bool` has no meaningful "empty" state — Go's zero value `false` is indistinguishable from "not set". Therefore:

- A plain `bool` field is **never** reported as missing, even if absent from the config file.
- If you need to enforce that a bool was **explicitly set**, use `*bool`:

```go
type Config struct {
    Enabled *bool // nil = not set → validation error; &false = explicit false → ok
}
```

### Pointer fields

Pointer types solve the "zero value vs not set" ambiguity for any type:

```go
type Config struct {
    Port    *int  // nil = not set; &0 = explicitly set to 0
    Enabled *bool // nil = not set; &false = explicitly set to false
}
```

A `nil` pointer is always considered empty (required field missing). A non-nil pointer is never empty, regardless of the pointed-to value.

## Environment Variables

### Inline expansion

`$ENV_VAR` and `${ENV_VAR}` references anywhere in the config file are expanded automatically before parsing:

```yaml
database:
  password: ${DB_PASSWORD}
  host: $DB_HOST
```

### Explicit field binding

Use `env=VAR_NAME` to bind a field directly to an env var. This takes priority over the file value:

```go
type Config struct {
    Secret string `config:",env=APP_SECRET"`
}
```

If `APP_SECRET` is not set in the environment, the value from the config file is used instead.

## Supported Types

| Go type              | Supported | Notes                                      |
|----------------------|-----------|--------------------------------------------|
| `string`             | ✅        |                                            |
| `bool`               | ✅        | `"true"`, `"false"`, `true`, `false`       |
| `int`, `int8..64`    | ✅        |                                            |
| `uint`, `uint8..64`  | ✅        |                                            |
| `float32`, `float64` | ✅        |                                            |
| `time.Duration`      | ✅        | `"5s"`, `"1m30s"` or nanoseconds as int   |
| `[]T` (slice)        | ✅        | Elements follow same conversion rules      |
| `map[K]V`            | ✅        | String keys recommended                    |
| pointer to any above | ✅        |                                            |
| nested struct        | ✅        | Mapped as a nested config object           |
| `interface{}`        | ✅        | Value passed through as-is                 |
| embedded struct      | ⚠️        | Treated as a named nested object, not flat |

## Nested Structs

Nested structs map to nested objects in the config file:

```go
type Config struct {
    Server   ServerConfig
    Database DatabaseConfig `config:",omitempty"`
}

type ServerConfig struct {
    Host string
    Port int
}

type DatabaseConfig struct {
    DSN string
}
```

```yaml
server:
  host: localhost
  port: 8080
# database is omitted — allowed because of omitempty
```

If `database` is present in the file, its fields are validated normally even with `omitempty`.

## Adding Custom Formats

Implement `Decoder` and register it for a file extension:

```go
type Decoder interface {
    Decode(data []byte) (map[string]interface{}, error)
}

// Register before calling InitFromFile.
rockconfig.RegisterDecoder(".toml", &MyTomlDecoder{})
```

`RegisterDecoder` is safe for concurrent use.

## Limitations

- **Embedded (anonymous) structs** are treated as named nested fields, not flattened. Use explicit field names instead.
- **`bool` zero value** cannot be distinguished from "not set". Use `*bool` to enforce presence.
- **`int` zero value** (e.g. port 0) is treated as missing. Use `*int` if 0 is a valid value.
- **`default=` must be last** in the tag option list.
- **`os.ExpandEnv`** is applied to the raw file before parsing. Env var values containing YAML/JSON special characters (quotes, braces, newlines) may corrupt the file structure.
