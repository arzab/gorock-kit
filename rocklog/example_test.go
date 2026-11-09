package rocklog_test

import (
	"context"
	"errors"
	"time"

	"github.com/arzab/gorock-kit/rocklog"
)

// Compiled, not run: mirrors the README.

func Example() {
	rocklog.Info("server started", rocklog.Int("port", 8080))
	rocklog.Error("request failed", rocklog.Err(errors.New("timeout")), rocklog.Str("path", "/api/v1"))

	rocklog.Init(rocklog.Config{
		Level:      rocklog.LevelInfo,
		Format:     rocklog.FormatJSON,
		TimeFormat: time.RFC3339,
		Caller:     true,
	})

	db := rocklog.Named("database").With(rocklog.Str("env", "prod"))
	db.Info("query executed", rocklog.Dur("took", 2*time.Millisecond), rocklog.Any("rows", 3))
}

func ExampleWithContext() {
	ctx := rocklog.WithContext(context.Background(), rocklog.Str("trace_id", "abc-123"))
	rocklog.FromContext(ctx).Info("user created", rocklog.Str("id", "42"))
}
