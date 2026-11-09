package rocklog

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestContextLoggerAccumulatesFields(t *testing.T) {
	var buf bytes.Buffer
	base := New(Config{Format: FormatJSON, Output: &buf})

	ctx := ContextWithLogger(context.Background(), base)
	ctx = WithContext(ctx, Str("trace_id", "t-1"))
	ctx = WithContext(ctx, Str("user", "u-7"))
	FromContext(ctx).Info("hello", Int("n", 1))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output %q: %v", buf.String(), err)
	}
	if entry["trace_id"] != "t-1" || entry["user"] != "u-7" || entry["n"] != float64(1) || entry["msg"] != "hello" {
		t.Fatalf("entry = %v", entry)
	}
}

func TestFromContextFallsBackToDefault(t *testing.T) {
	if FromContext(context.Background()) == nil {
		t.Fatal("FromContext must never return nil")
	}
	//nolint:staticcheck // nil ctx is tolerated on purpose
	if FromContext(nil) == nil {
		t.Fatal("FromContext(nil) must never return nil")
	}
}
