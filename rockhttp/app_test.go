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

	rh "github.com/arzab/gorock-kit/rockhandlers"
)

// --- a reusable step + embeddable model, as the catalog would ship them ---

type BearerAuth struct {
	Authorization string `header:"Authorization" json:"-"`
}

func (a *BearerAuth) BearerToken() string { return strings.TrimPrefix(a.Authorization, "Bearer ") }

type TokenParams interface{ BearerToken() string }

var UserKey = rh.NewLocal[string]("user")

func Bearer[P TokenParams, R any](c *rh.Ctx[P, R]) error {
	if c.Params.BearerToken() == "" {
		return rh.Unauthorized("token required")
	}
	UserKey.Set(c, c.Params.BearerToken())
	return c.Next()
}

// a group is a plain function
func Secured[P TokenParams, R any](name string, steps ...rh.Handler[P, R]) rh.Route {
	return rh.Chain(name, rh.Prepend(steps, Bearer[P, R])...)
}

// --- models and steps ---

type CreateParams struct {
	BearerAuth
	ShopID int      `path:"shop"`
	DryRun bool     `query:"dry_run"`
	Tags   []string `query:"tag"`
	Item   string   `json:"item"`
	Qty    int      `json:"qty"`
}

func (p *CreateParams) Validate() error {
	if p.Qty <= 0 {
		return rh.Invalid("invalid order").WithFields(map[string]string{"qty": "must be > 0"})
	}
	return nil
}

type CreateResult struct {
	Shop   int      `json:"shop"`
	Item   string   `json:"item"`
	DryRun bool     `json:"dry_run"`
	Tags   []string `json:"tags"`
	Owner  string   `json:"owner"`
}

func create(c *rh.Ctx[*CreateParams, *CreateResult]) error {
	p := c.Params
	*c.Result = CreateResult{Shop: p.ShopID, Item: p.Item, DryRun: p.DryRun, Tags: p.Tags, Owner: UserKey.MustGet(c)}
	return nil
}

type IDParams struct {
	ID int `path:"id"`
}
type Empty struct{}

// --- helpers ---

func handler(t *testing.T, a *App) http.Handler {
	t.Helper()
	if a.cfg.Port == "" {
		a.cfg.Port = "0"
	}
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

func jsonReq(method, url, body string) *http.Request {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

// --- tests ---

func TestFullChain(t *testing.T) {
	var log []string
	logMW := func(c rh.Context) error {
		err := c.Next()
		log = append(log, c.Route()+":"+rh.KindOf(err).String())
		return err
	}
	h := handler(t, New(Config{Prefix: "/api/"},
		Endpoint{Method: "POST", Path: "/shops/{shop}/orders", Handler: Secured("orders.create", create), Status: http.StatusCreated},
	).Use(logMW))

	req := jsonReq("POST", "/api/shops/7/orders?dry_run=true&tag=a&tag=b", `{"item":"tea","qty":2}`)
	req.Header.Set("Authorization", "Bearer ann")
	rec := do(h, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	got := decodeBody[CreateResult](t, rec)
	if got.Shop != 7 || got.Item != "tea" || !got.DryRun || len(got.Tags) != 2 || got.Owner != "ann" {
		t.Fatalf("got %+v", got)
	}

	// no token → 401 from the step that needs the embedded model
	rec = do(h, jsonReq("POST", "/api/shops/7/orders", `{"item":"tea","qty":2}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}

	// validation with fields
	req = jsonReq("POST", "/api/shops/7/orders", `{"item":"tea","qty":0}`)
	req.Header.Set("Authorization", "Bearer ann")
	rec = do(h, req)
	body := decodeBody[ErrorBody](t, rec)
	if rec.Code != 400 || body.Kind != "invalid" || body.Fields["qty"] != "must be > 0" {
		t.Fatalf("status %d, body %+v", rec.Code, body)
	}

	if got := strings.Join(log, " "); got != "orders.create:none orders.create:unauthorized orders.create:invalid" {
		t.Fatalf("middleware log: %s", got)
	}
}

func TestErrors(t *testing.T) {
	var reported []string
	logMW := func(c rh.Context) error {
		err := c.Next()
		if err != nil {
			reported = append(reported, c.Route())
		}
		return err
	}
	route := func(name string, fn rh.Handler[*IDParams, *Empty]) rh.Route { return rh.Chain(name, fn) }
	h := handler(t, New(Config{},
		Endpoint{Method: "GET", Path: "/missing/{id}", Handler: route("missing", func(c *rh.Ctx[*IDParams, *Empty]) error {
			return rh.NotFound("order %d not found", c.Params.ID)
		})},
		Endpoint{Method: "GET", Path: "/boom", Handler: route("boom", func(*rh.Ctx[*IDParams, *Empty]) error {
			return errors.New("db password leaked")
		})},
		Endpoint{Method: "GET", Path: "/panic", Handler: route("panic", func(*rh.Ctx[*IDParams, *Empty]) error { panic("oops") })},
		Endpoint{Method: "GET", Path: "/slow", Handler: route("slow", func(c *rh.Ctx[*IDParams, *Empty]) error {
			<-c.Done()
			return c.Err()
		}).With(rh.Timeout(10 * time.Millisecond))},
		Endpoint{Method: "DELETE", Path: "/orders/{id}", Handler: route("delete", func(*rh.Ctx[*IDParams, *Empty]) error { return nil })},
	).Use(logMW))

	cases := []struct {
		method, url string
		code        int
		kind, msg   string
	}{
		{"GET", "/missing/42", 404, "not_found", "order 42 not found"},
		{"GET", "/missing/x", 400, "invalid", `path "id": "x" is not an integer`},
		{"GET", "/boom", 500, "internal", "internal error"},
		{"GET", "/panic", 500, "internal", "internal error"},
		{"GET", "/slow", 504, "timeout", "Gateway Timeout"},
	}
	for _, c := range cases {
		rec := do(h, httptest.NewRequest(c.method, c.url, nil))
		body := decodeBody[ErrorBody](t, rec)
		if rec.Code != c.code || body.Kind != c.kind || body.Message != c.msg {
			t.Fatalf("%s %s: status %d, body %+v", c.method, c.url, rec.Code, body)
		}
	}
	if len(reported) != len(cases) {
		t.Fatalf("middleware saw errors of %v (decode errors included)", reported)
	}

	// empty result struct → 204
	if rec := do(h, httptest.NewRequest("DELETE", "/orders/1", nil)); rec.Code != 204 || rec.Body.Len() != 0 {
		t.Fatalf("delete: %d %q", rec.Code, rec.Body)
	}
	// method not allowed — net/http itself
	if rec := do(h, httptest.NewRequest("GET", "/orders/1", nil)); rec.Code != 405 {
		t.Fatalf("405: %d", rec.Code)
	}

	// client went away → 499, no body
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := do(h, httptest.NewRequest("GET", "/slow", nil).WithContext(ctx))
	if rec.Code != StatusClientClosedRequest || rec.Body.Len() != 0 {
		t.Fatalf("canceled: %d %q", rec.Code, rec.Body)
	}
}

// --- awkward cases: upload, download, redirect + cookie ---

type UploadParams struct {
	Title string                  `form:"title"`
	File  *multipart.FileHeader   `form:"file"`
	More  []*multipart.FileHeader `form:"more"`
}
type UploadResult struct {
	Title, Name, Body string
	More              int
}

func upload(c *rh.Ctx[*UploadParams, *UploadResult]) error {
	if c.Params.File == nil {
		return rh.Invalid("file is required")
	}
	f, err := c.Params.File.Open()
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	b, _ := io.ReadAll(f)
	*c.Result = UploadResult{Title: c.Params.Title, Name: c.Params.File.Filename, Body: string(b), More: len(c.Params.More)}
	return nil
}

func TestUpload(t *testing.T) {
	h := handler(t, New(Config{}, Endpoint{Method: "POST", Path: "/files", Handler: rh.Chain("upload", upload)}))

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

	req := httptest.NewRequest("POST", "/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := do(h, req)
	got := decodeBody[UploadResult](t, rec)
	if rec.Code != 200 || got.Title != "report" || got.Name != "a.txt" || got.Body != "hello" || got.More != 2 {
		t.Fatalf("status %d, got %+v", rec.Code, got)
	}
}

func TestBodyLimit(t *testing.T) {
	h := handler(t, New(Config{MaxBodyBytes: 10}, Endpoint{Method: "POST", Path: "/shops/{shop}/orders", Handler: rh.Chain("c", create)}))
	rec := do(h, jsonReq("POST", "/shops/1/orders", `{"item":"`+strings.Repeat("x", 100)+`","qty":1}`))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "larger than 10 bytes") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestDownload(t *testing.T) {
	h := handler(t, New(Config{}, Endpoint{Method: "GET", Path: "/files/{id}", Handler: rh.Chain("download",
		func(c *rh.Ctx[*IDParams, *File]) error {
			*c.Result = File{Name: "report.csv", Content: strings.NewReader("a,b\n1,2\n"), Download: true}
			return nil
		})}))

	rec := do(h, httptest.NewRequest("GET", "/files/1", nil))
	if rec.Code != 200 || rec.Body.String() != "a,b\n1,2\n" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") ||
		rec.Header().Get("Content-Disposition") != "attachment; filename=report.csv" {
		t.Fatalf("status %d, headers %v, body %q", rec.Code, rec.Header(), rec.Body)
	}
	req := httptest.NewRequest("GET", "/files/1", nil)
	req.Header.Set("Range", "bytes=0-2")
	if rec = do(h, req); rec.Code != http.StatusPartialContent || rec.Body.String() != "a,b" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body)
	}
}

type LoginParams struct {
	User string `form:"user"`
}

func TestRedirectAndCookie(t *testing.T) {
	h := handler(t, New(Config{}, Endpoint{Method: "POST", Path: "/login", Handler: rh.Chain("login",
		func(c *rh.Ctx[*LoginParams, *Redirect]) error {
			if c.Params.User == "" {
				return rh.Unauthorized("who are you?")
			}
			SetCookie(c, &http.Cookie{Name: "session", Value: "s-" + c.Params.User, HttpOnly: true})
			SetHeader(c, "X-User", c.Params.User)
			*c.Result = Redirect{URL: "/home", Code: http.StatusSeeOther}
			return nil
		})}))

	req := httptest.NewRequest("POST", "/login", strings.NewReader("user=ann"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := do(h, req)
	if rec.Code != 303 || rec.Header().Get("Location") != "/home" || rec.Header().Get("X-User") != "ann" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].Value != "s-ann" || !c[0].HttpOnly {
		t.Fatalf("cookies %v", c)
	}
}

func TestHTTPMiddleware(t *testing.T) {
	cors := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			next.ServeHTTP(w, r)
		})
	}
	h := handler(t, New(Config{Middleware: []func(http.Handler) http.Handler{cors}},
		Endpoint{Method: "DELETE", Path: "/x/{id}", Handler: rh.Chain("x", func(*rh.Ctx[*IDParams, *Empty]) error { return nil })}))
	if rec := do(h, httptest.NewRequest("DELETE", "/x/1", nil)); rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("headers %v", rec.Header())
	}
}

func TestInitErrors(t *testing.T) {
	ok := rh.Chain("x", func(*rh.Ctx[*IDParams, *Empty]) error { return nil })
	cases := map[string]*App{
		"no port":        New(Config{}, Endpoint{Method: "GET", Path: "/a", Handler: ok}),
		"no handler":     New(Config{Port: "0"}, Endpoint{Method: "GET", Path: "/a"}),
		"no method":      New(Config{Port: "0"}, Endpoint{Path: "/a", Handler: ok}),
		"duplicate":      New(Config{Port: "0"}, Endpoint{Method: "GET", Path: "/a", Handler: ok}, Endpoint{Method: "GET", Path: "/a", Handler: ok}),
		"nil middleware": New(Config{Port: "0"}, Endpoint{Method: "GET", Path: "/a", Handler: ok}).Use(nil),
	}
	for name, a := range cases {
		if err := a.Init(context.Background()); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestExecAndStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()

	a := New(Config{Port: port, ShutdownTimeout: time.Second},
		Endpoint{Method: "GET", Path: "/ping/{id}", Handler: rh.Chain("ping", func(*rh.Ctx[*IDParams, *Empty]) error { return nil })})
	if err := a.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Exec(ctx) }()

	var resp *http.Response
	for range 50 {
		if resp, err = http.Get("http://127.0.0.1:" + port + "/ping/1"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server did not start: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("status %d", resp.StatusCode)
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
