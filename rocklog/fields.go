package rocklog

import (
	"fmt"
	"time"
)

// Field is a structured key-value pair attached to a log entry.
type Field struct {
	Key   string
	Value any
}

// F creates a Field with any value.
func F(key string, val any) Field { return Field{Key: key, Value: val} }

// Err creates an error Field with key "error".
// If err is nil, Value will be nil and logged as null — use omitempty patterns in your formatter if needed.
func Err(err error) Field { return Field{Key: "error", Value: err} }

// Str creates a string Field.
func Str(key, val string) Field { return Field{Key: key, Value: val} }

// Int creates an int Field.
func Int(key string, val int) Field { return Field{Key: key, Value: val} }

// Int64 creates an int64 Field.
func Int64(key string, val int64) Field { return Field{Key: key, Value: val} }

// Float64 creates a float64 Field.
func Float64(key string, val float64) Field { return Field{Key: key, Value: val} }

// Bool creates a bool Field.
func Bool(key string, val bool) Field { return Field{Key: key, Value: val} }

// Dur creates a time.Duration Field. The value is stored as a human-readable string
// (e.g. "1m30s") so it remains readable in both text and JSON output.
func Dur(key string, val time.Duration) Field { return Field{Key: key, Value: val.String()} }

// Time creates a time.Time Field.
func Time(key string, val time.Time) Field { return Field{Key: key, Value: val} }

// Stringer creates a Field from any fmt.Stringer.
// If val is nil, the field value will be the string "<nil>".
func Stringer(key string, val fmt.Stringer) Field {
	if val == nil {
		return Field{Key: key, Value: "<nil>"}
	}
	return Field{Key: key, Value: val.String()}
}
