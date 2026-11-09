package rockhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arzab/gorock-kit/rockdelivery"
)

type createIn struct {
	ShopID int      `path:"shop"`
	DryRun bool     `query:"dry_run"`
	Tags   []string `query:"tag"`
	Token  string   `header:"X-Token"`
	Item   string   `json:"item"`
	Qty    int      `json:"qty"`
}

func (in *createIn) Validate() error {
	if in.Qty <= 0 {
		return errors.New("qty must be > 0")
	}
	return nil
}

type createOut struct {
	Shop   int      `json:"shop"`
	Item   string   `json:"item"`
	DryRun bool     `json:"dry_run"`
	Tags   []string `json:"tags"`
	Token  string   `json:"token"`
}

func create(_ context.Context, in *createIn) (*createOut, error) {
	return &createOut{Shop: in.ShopID, Item: in.Item, DryRun: in.DryRun, Tags: in.Tags, Token: in.Token}, nil
}

type idIn struct {
	ID int `path:"id"`
}

type uploadIn struct {
	Title string                  `form:"title"`
	File  *multipart.FileHeader   `form:"file"`
	More  []*multipart.FileHeader `form:"more"`
}

type uploadOut struct {
	Title string `json:"title"`
	Name  string `json:"name"`
	Body  string `json:"body"`
	More  int    `json:"more"`
}

func upload(_ context.Context, in *uploadIn) (*uploadOut, error) {
	if in.File == nil {
		return nil, rockdelivery.Invalid("file is required")
	}
	f, err := in.File.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, _ := io.ReadAll(f)
	return &uploadOut{Title: in.Title, Name: in.File.Filename, Body: string(b), More: len(in.More)}, nil
}

type nothing struct{}

func newTestApp(t *testing.T, cfg Config, eps ...Endpoint) http.Handler {
	t.Helper()
	if cfg.Port == "" {
		cfg.Port = "0"
	}
	a := New(cfg, eps...)
	if err := a.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return a.Handler()
}

func do(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestBindAllSources(t *testing.T) {
	h := newTestApp(t, Config{Prefix: "/api/"},
		POST("/shops/{shop}/orders", create, Status(http.StatusCreated)))

	req := httptest.NewRequest(http.MethodPost, "/api/shops/7/orders?dry_run=true&tag=a&tag=b",
		strings.NewReader(`{"item":"tea","qty":2}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-Token", "secret")
	rec := do(h, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	got := decodeBody[createOut](t, rec)
	if got.Shop != 7 || got.Item != "tea" || !got.DryRun || len(got.Tags) != 2 || got.Token != "secret" {
		t.Fatalf("got %+v", got)
	}
}

func TestErrors(t *testing.T) {
	var failures []rockdelivery.Failure
	h := newTestApp(t, Config{OnFailure: func(_ context.Context, f rockdelivery.Failure) { failures = append(failures, f) }},
		POST("/shops/{shop}/orders", create),
		GET("/missing/{id}", func(_ context.Context, in *idIn) (*nothing, error) {
			return nil, rockdelivery.NotFound("order %d not found", in.ID)
		}),
		GET("/boom", func(context.Context, *nothing) (*nothing, error) {
			return nil, errors.New("db password leaked")
		}),
		GET("/panic", func(context.Context, *nothing) (*nothing, error) { panic("oops") }),
	)

	cases := []struct {
		name, method, url, body string
		code                    int
		kind, msg               string
	}{
		{"validate", http.MethodPost, "/shops/1/orders", `{"qty":0}`, 400, "invalid", "qty must be > 0"},
		{"bad json", http.MethodPost, "/shops/1/orders", `{"qty":`, 400, "invalid", ""},
		{"bad path", http.MethodPost, "/shops/x/orders", `{"qty":1}`, 400, "invalid", `path "shop": "x" is not an integer`},
		{"not found", http.MethodGet, "/missing/42", "", 404, "not_found", "order 42 not found"},
		{"internal hidden", http.MethodGet, "/boom", "", 500, "internal", "internal error"},
		{"panic hidden", http.MethodGet, "/panic", "", 500, "internal", "internal error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.url, strings.NewReader(c.body))
			if c.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := do(h, req)
			if rec.Code != c.code {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			got := decodeBody[ErrorBody](t, rec)
			if got.Kind != c.kind || (c.msg != "" && got.Message != c.msg) {
				t.Fatalf("got %+v", got)
			}
		})
	}
	if len(failures) != len(cases) {
		t.Fatalf("failures reported = %d, want %d", len(failures), len(cases))
	}
}

func TestNilResultIsNoContent(t *testing.T) {
	h := newTestApp(t, Config{},
		DELETE("/orders/{id}", func(context.Context, *idIn) (*nothing, error) { return nil, nil }))
	rec := do(h, httptest.NewRequest(http.MethodDelete, "/orders/1", nil))
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestApp(t, Config{}, DELETE("/orders/{id}", func(context.Context, *idIn) (*nothing, error) { return nil, nil }))
	if rec := do(h, httptest.NewRequest(http.MethodGet, "/orders/1", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestUpload(t *testing.T) {
	h := newTestApp(t, Config{}, POST("/files", upload))

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "report")
	fw, _ := mw.CreateFormFile("file", "a.txt")
	_, _ = fw.Write([]byte("hello"))
	for _, n := range []string{"b.txt", "c.txt"} {
		fw, _ = mw.CreateFormFile("more", n)
		_, _ = fw.Write([]byte(n))
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := do(h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	got := decodeBody[uploadOut](t, rec)
	if got.Title != "report" || got.Name != "a.txt" || got.Body != "hello" || got.More != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestBodyLimit(t *testing.T) {
	h := newTestApp(t, Config{MaxBodyBytes: 10}, POST("/shops/{shop}/orders", create))
	req := httptest.NewRequest(http.MethodPost, "/shops/1/orders", strings.NewReader(`{"item":"`+strings.Repeat("x", 100)+`","qty":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := do(h, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "larger than 10 bytes") {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestDownload(t *testing.T) {
	h := newTestApp(t, Config{},
		GET("/files/{id}", func(context.Context, *idIn) (*File, error) {
			return &File{Name: "report.csv", Content: strings.NewReader("a,b\n1,2\n"), Download: true}, nil
		}))

	rec := do(h, httptest.NewRequest(http.MethodGet, "/files/1", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "a,b\n1,2\n" {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename=report.csv` {
		t.Fatalf("Content-Disposition = %q", cd)
	}

	// strings.Reader is a ReadSeeker: ranges work.
	req := httptest.NewRequest(http.MethodGet, "/files/1", nil)
	req.Header.Set("Range", "bytes=0-2")
	if rec = do(h, req); rec.Code != http.StatusPartialContent || rec.Body.String() != "a,b" {
		t.Fatalf("range: status = %d, body %q", rec.Code, rec.Body)
	}
}

func TestRedirectAndCookie(t *testing.T) {
	type loginIn struct {
		User string `form:"user"`
	}
	h := newTestApp(t, Config{},
		POST("/login", func(ctx context.Context, in *loginIn) (*Redirect, error) {
			if in.User == "" {
				return nil, rockdelivery.Unauthorized("who are you?")
			}
			SetCookie(ctx, &http.Cookie{Name: "session", Value: "s-" + in.User, HttpOnly: true})
			SetHeader(ctx, "X-User", in.User)
			if Request(ctx) == nil {
				return nil, errors.New("no raw request")
			}
			return &Redirect{URL: "/home", Code: http.StatusSeeOther}, nil
		}))

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("user=ann"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := do(h, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/home" {
		t.Fatalf("status = %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].Value != "s-ann" || !c[0].HttpOnly {
		t.Fatalf("cookies = %v", c)
	}
	if rec.Header().Get("X-User") != "ann" {
		t.Fatalf("X-User = %q", rec.Header().Get("X-User"))
	}

	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec = do(h, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHTTPMiddleware(t *testing.T) {
	cors := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			next.ServeHTTP(w, r)
		})
	}
	h := newTestApp(t, Config{Middleware: []func(http.Handler) http.Handler{cors}},
		GET("/ping", func(context.Context, *nothing) (*map[string]string, error) {
			return &map[string]string{"pong": "ok"}, nil
		}))
	rec := do(h, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("status = %d, headers %v", rec.Code, rec.Header())
	}
}

func TestInitErrors(t *testing.T) {
	h := func(context.Context, *nothing) (*nothing, error) { return nil, nil }
	if err := New(Config{}, GET("/a", h)).Init(context.Background()); err == nil {
		t.Fatal("empty Port: want error")
	}
	err := New(Config{Port: "0"}, GET("/a", h), GET("/a", h)).Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rockhttp:") {
		t.Fatalf("duplicate route: err = %v", err)
	}
}

func TestExecAndStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	a := New(Config{Port: strconv.Itoa(port), ShutdownTimeout: time.Second},
		GET("/ping", func(context.Context, *nothing) (*nothing, error) { return &nothing{}, nil }))
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Exec(ctx) }()

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/ping"
	var resp *http.Response
	for i := 0; i < 50; i++ {
		if resp, err = http.Get(url); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server did not start: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Exec = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Exec did not return after cancel")
	}
	if errs := a.Stop(); errs != nil {
		t.Fatalf("Stop = %v", errs)
	}
}
