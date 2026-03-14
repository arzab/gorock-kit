package rockconfig

import (
	"fmt"
	"reflect"
	"strings"
)

// InitFromFile reads the config file at path and returns a populated Config struct.
//
// Supported formats: .json, .yaml, .yml (extensible via RegisterDecoder).
//
// Field mapping:
//   - By default, struct fields are mapped by their snake_case name.
//   - Use the config tag to customise behaviour:
//
//	type Config struct {
//	    Host    string `config:"host"`             // explicit name
//	    Port    int    `config:",default=8080"`     // snake_case + default
//	    Debug   bool   `config:",omitempty"`        // snake_case, not required
//	    Secret  string `config:",env=APP_SECRET"`   // read from $APP_SECRET
//	    Token   string `config:"-"`                 // ignored
//	}
//
// $ENV_VAR references in the config file are expanded automatically.
// Returns an error listing all missing required fields if any are empty after loading.
func InitFromFile[C any](path string) (*C, error) {
	cfg := new(C)
	v := reflect.ValueOf(cfg).Elem()
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("rockconfig: type parameter C must be a struct, got %s", v.Kind())
	}

	data, err := load(path)
	if err != nil {
		return nil, err
	}

	if err := populate(data, v); err != nil {
		return nil, fmt.Errorf("rockconfig: populate: %w", err)
	}

	rootName := reflect.TypeOf(*cfg).Name()
	if rootName == "" {
		rootName = "Config"
	}
	if issues := validate(v, rootName); len(issues) > 0 {
		return nil, fmt.Errorf("rockconfig: missing required fields:\n  %s", strings.Join(issues, "\n  "))
	}

	return cfg, nil
}
