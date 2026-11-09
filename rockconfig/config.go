package rockconfig

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// ErrMissingFields is wrapped by the error returned when required fields are absent.
var ErrMissingFields = errors.New("missing required fields")

// ErrUnknownKeys is wrapped by the error returned when the file contains keys
// that do not map to any struct field. Disable with AllowUnknownKeys.
var ErrUnknownKeys = errors.New("unknown keys")

// Option customises InitFromFile.
type Option func(*options)

type options struct {
	allowUnknown bool
}

// AllowUnknownKeys makes InitFromFile ignore keys that do not map to any
// struct field instead of returning ErrUnknownKeys. Use it when one file is
// shared by several programs that each read only part of it.
func AllowUnknownKeys() Option {
	return func(o *options) { o.allowUnknown = true }
}

// InitFromFile reads the config file at path and returns a populated Config struct.
//
// Supported formats: .json, .yaml, .yml (extensible via RegisterDecoder).
//
// Field mapping:
//
//   - By default, struct fields are mapped by their snake_case name.
//
//   - Use the config tag to customise behaviour:
//
//     type Config struct {
//     Host    string `config:"host"`             // explicit name
//     Port    int    `config:",default=8080"`     // snake_case + default
//     Debug   bool   `config:",omitempty"`        // snake_case, not required
//     Secret  string `config:",env=APP_SECRET"`   // read from $APP_SECRET
//     Token   string `config:"-"`                 // ignored
//     }
//
// String values may reference environment variables as ${VAR} or
// ${VAR:-fallback}. An unset ${VAR} without fallback is an error. Write $${
// for a literal "${". A bare $ is never expanded.
//
// A required field must be present in the file (or provided by env= or
// default=); its value may be zero (port: 0, enabled: false), except that a
// required string must not be empty. Keys that match no field are an error
// unless AllowUnknownKeys is passed. All problems are reported together.
func InitFromFile[C any](path string, opts ...Option) (*C, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	cfg := new(C)
	v := reflect.ValueOf(cfg).Elem()
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("rockconfig: type parameter C must be a struct, got %s", v.Kind())
	}

	data, err := load(path)
	if err != nil {
		return nil, err
	}

	expanded, err := expandEnvTree(data, "")
	if err != nil {
		return nil, fmt.Errorf("rockconfig: %s: %w", path, err)
	}

	l := &loader{opts: o}
	_, missing, err := l.populateStruct(expanded.(map[string]interface{}), v, "")
	if err != nil {
		return nil, fmt.Errorf("rockconfig: %s: %w", path, err)
	}
	l.missing = append(missing, l.missing...)

	var errs []error
	if len(l.missing) > 0 {
		errs = append(errs, fmt.Errorf("%w:\n  %s", ErrMissingFields, strings.Join(l.missing, "\n  ")))
	}
	if len(l.unknown) > 0 && !o.allowUnknown {
		errs = append(errs, fmt.Errorf("%w (typo? use rockconfig.AllowUnknownKeys() to ignore):\n  %s", ErrUnknownKeys, strings.Join(l.unknown, "\n  ")))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("rockconfig: %s: %w", path, errors.Join(errs...))
	}
	return cfg, nil
}
