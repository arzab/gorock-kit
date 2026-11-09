package rockconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func loadYAML[C any](t *testing.T, content string, opts ...Option) (*C, error) {
	t.Helper()
	return InitFromFile[C](writeFile(t, "config.yaml", content), opts...)
}

func mustLoad[C any](t *testing.T, content string, opts ...Option) *C {
	t.Helper()
	cfg, err := loadYAML[C](t, content, opts...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return cfg
}

func wantErr(t *testing.T, err error, substrings ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substrings)
	}
	for _, s := range substrings {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error %q does not contain %q", err, s)
		}
	}
}

// ---------- 4.1 env expansion ----------

func TestEnvExpansionInStrings(t *testing.T) {
	t.Setenv("RC_HOST", "db.local")
	t.Setenv("RC_PORT", "6432")
	type C struct {
		Host string
		Port int
		URL  string
	}
	cfg := mustLoad[C](t, "host: ${RC_HOST}\nport: ${RC_PORT}\nurl: \"postgres://${RC_HOST}:${RC_PORT}/x\"\n")
	if cfg.Host != "db.local" || cfg.Port != 6432 || cfg.URL != "postgres://db.local:6432/x" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// Regression: os.ExpandEnv on the raw file turned "pa$$word" into "paword"
// and silently replaced unset variables with "".
func TestDollarSignsAreLiteral(t *testing.T) {
	type C struct {
		Password string
		Hash     string
		Template string
	}
	cfg := mustLoad[C](t, "password: pa$$word\nhash: \"$2a$10$abcdef\"\ntemplate: \"$${NOT_EXPANDED}\"\n")
	if cfg.Password != "pa$$word" || cfg.Hash != "$2a$10$abcdef" || cfg.Template != "${NOT_EXPANDED}" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestUnsetEnvIsError(t *testing.T) {
	type C struct{ Password string }
	_, err := loadYAML[C](t, "password: ${RC_DEFINITELY_UNSET}\n")
	wantErr(t, err, "RC_DEFINITELY_UNSET is not set", "password")
}

func TestEnvFallback(t *testing.T) {
	t.Setenv("RC_EMPTY", "")
	type C struct{ A, B string }
	cfg := mustLoad[C](t, "a: ${RC_UNSET_X:-fallback}\nb: ${RC_EMPTY:-used-when-empty}\n")
	if cfg.A != "fallback" || cfg.B != "used-when-empty" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// Regression: env values were spliced into the raw YAML, so a value with a
// newline or colon could inject keys.
func TestEnvValueCannotChangeStructure(t *testing.T) {
	t.Setenv("RC_EVIL", "x\nadmin: true")
	type C struct {
		Name  string
		Admin bool `config:",omitempty"`
	}
	cfg := mustLoad[C](t, "name: ${RC_EVIL}\n")
	if cfg.Admin || cfg.Name != "x\nadmin: true" {
		t.Fatalf("env value changed structure: %+v", cfg)
	}
}

func TestEnvInListsAndJSON(t *testing.T) {
	t.Setenv("RC_ITEM", "b")
	type C struct{ Items []string }
	cfg, err := InitFromFile[C](writeFile(t, "c.json", `{"items": ["a", "${RC_ITEM}"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Items, ",") != "a,b" {
		t.Fatalf("items = %v", cfg.Items)
	}
}

func TestInvalidEnvReference(t *testing.T) {
	type C struct{ A string }
	_, err := loadYAML[C](t, "a: \"${not valid}\"\n")
	wantErr(t, err, "invalid env reference")
	_, err = loadYAML[C](t, "a: \"${OPEN\"\n")
	wantErr(t, err, "unterminated")
}

// ---------- env= binding (unchanged behaviour) ----------

func TestEnvBindingOverridesFile(t *testing.T) {
	t.Setenv("RC_SECRET", "from-env")
	type C struct {
		Secret string `config:",env=RC_SECRET"`
	}
	cfg := mustLoad[C](t, "secret: from-file\n")
	if cfg.Secret != "from-env" {
		t.Fatalf("secret = %q", cfg.Secret)
	}
}

// ---------- 4.2 absent nested sections ----------

type dbSection struct {
	Host     string        `config:",default=localhost"`
	Password string        `config:",env=RC_DB_PASSWORD"`
	Timeout  time.Duration `config:",default=5s"`
}

// Regression: when the section was missing from the file, populate never
// entered the nested struct, so its defaults and env bindings were ignored.
func TestDefaultsAndEnvApplyInAbsentSection(t *testing.T) {
	t.Setenv("RC_DB_PASSWORD", "s3cret")
	type C struct {
		Name string
		DB   dbSection
	}
	cfg := mustLoad[C](t, "name: app\n")
	if cfg.DB.Host != "localhost" || cfg.DB.Password != "s3cret" || cfg.DB.Timeout != 5*time.Second {
		t.Fatalf("db = %+v", cfg.DB)
	}
}

func TestAbsentRequiredSectionReportsFields(t *testing.T) {
	type C struct {
		DB struct {
			Host string
			Port int
		}
	}
	_, err := loadYAML[C](t, "{}\n")
	wantErr(t, err, "db.host", "db.port")
	if !errors.Is(err, ErrMissingFields) {
		t.Fatalf("err does not wrap ErrMissingFields: %v", err)
	}
}

func TestAbsentOptionalSection(t *testing.T) {
	type C struct {
		Name string
		DB   struct{ Host string } `config:",omitempty"`
	}
	mustLoad[C](t, "name: app\n")

	// Present optional section is validated normally.
	_, err := loadYAML[C](t, "name: app\ndb: {other: 1}\n", AllowUnknownKeys())
	wantErr(t, err, "db.host")
}

func TestPointerSection(t *testing.T) {
	type C struct {
		TLS *struct {
			Cert string
			Key  string
		} `config:",omitempty"`
	}
	if cfg := mustLoad[C](t, "{}\n"); cfg.TLS != nil {
		t.Fatal("absent optional pointer section must stay nil")
	}
	cfg := mustLoad[C](t, "tls: {cert: a, key: b}\n")
	if cfg.TLS == nil || cfg.TLS.Cert != "a" {
		t.Fatalf("tls = %+v", cfg.TLS)
	}

	type R struct{ TLS *struct{ Cert string } }
	_, err := loadYAML[R](t, "{}\n")
	wantErr(t, err, "tls")
}

func TestPointerSectionAllocatedByEnv(t *testing.T) {
	t.Setenv("RC_PTR_KEY", "k")
	type C struct {
		Auth *struct {
			Key string `config:",env=RC_PTR_KEY"`
		} `config:",omitempty"`
	}
	cfg := mustLoad[C](t, "{}\n")
	if cfg.Auth == nil || cfg.Auth.Key != "k" {
		t.Fatalf("auth = %+v", cfg.Auth)
	}
}

// ---------- 4.3 presence instead of zero values ----------

// Regression: 0 and false were treated as "missing".
func TestZeroValuesSatisfyRequired(t *testing.T) {
	type C struct {
		Port    int
		Ratio   float64
		Enabled bool
		Tags    []string
	}
	cfg := mustLoad[C](t, "port: 0\nratio: 0\nenabled: false\ntags: []\n")
	if cfg.Port != 0 || cfg.Enabled {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestRequiredBoolMustBePresent(t *testing.T) {
	type C struct{ Enabled bool }
	_, err := loadYAML[C](t, "{}\n")
	wantErr(t, err, "enabled")
}

func TestRequiredStringMustNotBeEmpty(t *testing.T) {
	type C struct{ Host string }
	_, err := loadYAML[C](t, "host: \"\"\n")
	wantErr(t, err, "host (empty)")
}

func TestNullCountsAsAbsent(t *testing.T) {
	type C struct{ Host string }
	_, err := loadYAML[C](t, "host:\n")
	wantErr(t, err, "host")
}

// ---------- 4.4 unknown keys ----------

func TestUnknownKeysAreReported(t *testing.T) {
	type C struct {
		HTTP struct {
			RequestTimeout time.Duration `config:",omitempty"`
		}
	}
	_, err := loadYAML[C](t, "http:\n  request_timout: 5s\nextra: 1\n")
	wantErr(t, err, "http.request_timout", "extra")
	if !errors.Is(err, ErrUnknownKeys) {
		t.Fatalf("err does not wrap ErrUnknownKeys: %v", err)
	}
}

func TestAllowUnknownKeys(t *testing.T) {
	type C struct{ Name string }
	cfg := mustLoad[C](t, "name: x\nother_service: {a: 1}\n", AllowUnknownKeys())
	if cfg.Name != "x" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestIgnoredFieldKeyIsUnknown(t *testing.T) {
	type C struct {
		Name  string
		Token string `config:"-"`
	}
	_, err := loadYAML[C](t, "name: x\ntoken: y\n")
	wantErr(t, err, "token")
}

func TestMissingAndUnknownReportedTogether(t *testing.T) {
	type C struct{ Host string }
	_, err := loadYAML[C](t, "hots: x\n")
	if !errors.Is(err, ErrMissingFields) || !errors.Is(err, ErrUnknownKeys) {
		t.Fatalf("expected both errors, got %v", err)
	}
}

// ---------- 4.5 durations ----------

// Regression: "timeout: 30" became 30 nanoseconds.
func TestNumericDurationIsError(t *testing.T) {
	type C struct{ Timeout time.Duration }
	_, err := loadYAML[C](t, "timeout: 30\n")
	wantErr(t, err, "timeout", "with a unit")

	cfg := mustLoad[C](t, "timeout: 1m30s\n")
	if cfg.Timeout != 90*time.Second {
		t.Fatalf("timeout = %v", cfg.Timeout)
	}
}

// ---------- 4.6 empty value with default ----------

func TestEmptyValueUsesDefault(t *testing.T) {
	type C struct {
		Host string `config:",default=localhost"`
	}
	cfg := mustLoad[C](t, "host: \"\"\n")
	if cfg.Host != "localhost" {
		t.Fatalf("host = %q", cfg.Host)
	}
}

func TestDefaultWithCommas(t *testing.T) {
	type C struct {
		List string `config:",default=a,b,c"`
	}
	if cfg := mustLoad[C](t, "{}\n"); cfg.List != "a,b,c" {
		t.Fatalf("list = %q", cfg.List)
	}
}

// ---------- 4.7 overflow ----------

func TestOverflow(t *testing.T) {
	type I struct{ N int8 }
	_, err := loadYAML[I](t, "n: 300\n")
	wantErr(t, err, "overflows int8")

	type U struct{ N uint }
	_, err = loadYAML[U](t, "n: -1\n")
	wantErr(t, err, "negative")

	type U8 struct{ N uint8 }
	_, err = loadYAML[U8](t, "n: 256\n")
	wantErr(t, err, "overflows uint8")

	type F struct{ N float32 }
	_, err = loadYAML[F](t, "n: 1e40\n")
	wantErr(t, err, "overflows float32")
}

func TestJSONFloatToInt(t *testing.T) {
	type C struct{ N int }
	cfg, err := InitFromFile[C](writeFile(t, "c.json", `{"n": 42}`))
	if err != nil || cfg.N != 42 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	_, err = InitFromFile[C](writeFile(t, "c.json", `{"n": 4.5}`))
	wantErr(t, err, "without loss")
}

// ---------- types and structure ----------

func TestCollections(t *testing.T) {
	type Item struct {
		Name string
		Size int `config:",default=1"`
	}
	type C struct {
		Items  []Item
		Limits map[string]int
		Ptr    *int
		Any    interface{} `config:",omitempty"`
	}
	cfg := mustLoad[C](t, "items:\n  - name: a\n  - name: b\n    size: 3\nlimits: {x: 1, y: 2}\nptr: 0\nany: [1, two]\n")
	if len(cfg.Items) != 2 || cfg.Items[0].Size != 1 || cfg.Items[1].Size != 3 {
		t.Fatalf("items = %+v", cfg.Items)
	}
	if cfg.Limits["y"] != 2 || cfg.Ptr == nil || *cfg.Ptr != 0 || cfg.Any == nil {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestMissingFieldInSliceElement(t *testing.T) {
	type Item struct{ Name string }
	type C struct{ Items []Item }
	_, err := loadYAML[C](t, "items:\n  - name: a\n  - {}\n")
	wantErr(t, err, "items[1].name")
}

func TestUnknownKeyInSliceElement(t *testing.T) {
	type Item struct{ Name string }
	type C struct{ Items []Item }
	_, err := loadYAML[C](t, "items:\n  - name: a\n    nmae: b\n")
	wantErr(t, err, "items[0].nmae")
}

func TestPortAsIntIntoString(t *testing.T) {
	type C struct{ Port string }
	if cfg := mustLoad[C](t, "port: 8080\n"); cfg.Port != "8080" {
		t.Fatalf("port = %q", cfg.Port)
	}
}

func TestSnakeCaseAndExplicitNames(t *testing.T) {
	type C struct {
		HTTPServer string
		UserID     int
		Custom     string `config:"renamed"`
	}
	cfg := mustLoad[C](t, "http_server: a\nuser_id: 7\nrenamed: c\n")
	if cfg.HTTPServer != "a" || cfg.UserID != 7 || cfg.Custom != "c" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestEmptyFile(t *testing.T) {
	type C struct {
		Name string `config:",default=x"`
	}
	if cfg := mustLoad[C](t, ""); cfg.Name != "x" {
		t.Fatalf("name = %q", cfg.Name)
	}
}

func TestTypeErrorsNamePath(t *testing.T) {
	type C struct {
		DB struct{ Port int }
	}
	_, err := loadYAML[C](t, "db:\n  port: abc\n")
	wantErr(t, err, "db.port", "cannot parse")
	_, err = loadYAML[C](t, "db: 5\n")
	wantErr(t, err, "db", "expected an object")
}

func TestUnsupportedExtension(t *testing.T) {
	type C struct{}
	_, err := InitFromFile[C](writeFile(t, "c.toml", "a = 1"))
	wantErr(t, err, "unsupported config file extension")
}

func TestNonStructType(t *testing.T) {
	_, err := InitFromFile[int](writeFile(t, "c.yaml", "a: 1"))
	wantErr(t, err, "must be a struct")
}

// ---------- tag parsing (unchanged) ----------

func TestToSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"MyField": "my_field", "HTTPServer": "http_server", "UserID": "user_id", "A": "a", "ID": "id",
	} {
		if got := toSnakeCase(in); got != want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}
