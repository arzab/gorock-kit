package rocklog

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func jsonLogger(buf *bytes.Buffer, cfg Config) Logger {
	cfg.Format = FormatJSON
	cfg.Output = buf
	return New(cfg)
}

// entries parses one JSON object per line.
func entries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestLevelNamesAndFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := jsonLogger(&buf, Config{Level: LevelInfo})
	l.Debug("hidden")
	l.Info("i")
	l.Warn("w")
	l.Error("e")

	got := entries(t, &buf)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3 (debug filtered): %v", len(got), got)
	}
	for i, want := range []string{"info", "warning", "error"} {
		if got[i]["level"] != want {
			t.Fatalf("entry %d level = %v, want %q", i, got[i]["level"], want)
		}
	}
	if l.IsEnabled(LevelDebug) || !l.IsEnabled(LevelWarn) {
		t.Fatal("IsEnabled mismatch")
	}
}

func TestFieldsAndErrors(t *testing.T) {
	var buf bytes.Buffer
	l := jsonLogger(&buf, Config{})
	l.Info("msg", Str("s", "v"), Int("n", 3), Bool("b", true), Err(errors.New("boom")), Dur("d", 1500*time.Millisecond))
	e := entries(t, &buf)[0]
	if e["msg"] != "msg" || e["s"] != "v" || e["n"] != float64(3) || e["b"] != true || e["error"] != "boom" || e["d"] != "1.5s" {
		t.Fatalf("entry = %v", e)
	}
}

// With and Named replace an existing key instead of emitting it twice
// (logrus semantics, which slog does not provide by itself).
func TestWithAndNamedReplaceKeys(t *testing.T) {
	var buf bytes.Buffer
	l := jsonLogger(&buf, Config{}).
		With(Str("k", "old"), Str("a", "1")).
		With(Str("k", "new")).
		Named("first").
		Named("second")
	l.Info("x", Str("a", "call-wins"))

	line := strings.TrimSpace(buf.String())
	if strings.Count(line, `"k"`) != 1 || strings.Count(line, `"logger"`) != 1 || strings.Count(line, `"a"`) != 1 {
		t.Fatalf("duplicate keys in %s", line)
	}
	e := entries(t, &buf)[0]
	if e["k"] != "new" || e["logger"] != "second" || e["a"] != "call-wins" {
		t.Fatalf("entry = %v", e)
	}
}

func TestWithDoesNotMutateParent(t *testing.T) {
	var buf bytes.Buffer
	parent := jsonLogger(&buf, Config{}).With(Str("p", "1"))
	_ = parent.With(Str("child", "1"))
	parent.Info("x")
	if _, ok := entries(t, &buf)[0]["child"]; ok {
		t.Fatal("child field leaked into parent")
	}
}

func TestTimeFormat(t *testing.T) {
	var buf bytes.Buffer
	jsonLogger(&buf, Config{TimeFormat: "2006-01-02"}).Info("x")
	ts, _ := entries(t, &buf)[0]["time"].(string)
	if _, err := time.Parse("2006-01-02", ts); err != nil {
		t.Fatalf("time %q not in custom format", ts)
	}
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	New(Config{Output: &buf}).Warn("hello", Str("k", "v"))
	out := buf.String()
	for _, want := range []string{"level=warning", "msg=hello", "k=v"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output %q missing %q", out, want)
		}
	}
}

// The caller must point at this file for every usage path.
func TestCallerReportsUserCode(t *testing.T) {
	var buf bytes.Buffer
	check := func(label string) {
		t.Helper()
		c, _ := entries(t, &buf)[0]["caller"].(string)
		if !strings.HasPrefix(c, "slog_test.go:") {
			t.Fatalf("%s: caller = %q, want slog_test.go:<line>", label, c)
		}
		buf.Reset()
	}

	l := jsonLogger(&buf, Config{Caller: true})
	l.Info("x")
	check("instance")

	l.With(Str("a", "b")).Info("x")
	check("With")

	orig := Default()
	t.Cleanup(func() { SetDefault(orig) })
	Init(Config{Format: FormatJSON, Output: &buf, Caller: true})
	Info("x")
	check("package-level")

	Named("svc").Info("x")
	check("package Named")
}

func TestFatalExits(t *testing.T) {
	var code int
	orig := exit
	exit = func(c int) { code = c }
	t.Cleanup(func() { exit = orig })

	var buf bytes.Buffer
	jsonLogger(&buf, Config{}).Fatal("bye")
	if code != 1 || entries(t, &buf)[0]["level"] != "fatal" {
		t.Fatalf("code=%d out=%s", code, buf.String())
	}

	// Fatal exits even when the level is filtered out.
	code = 0
	jsonLogger(&buf, Config{Level: Level(99)}).Fatal("bye")
	if code != 1 {
		t.Fatal("Fatal must exit regardless of level")
	}
}
