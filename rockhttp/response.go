package rockhttp

import (
	"context"
	"io"
	"net/http"
	"time"
)

// File is a result that streams a file instead of JSON. Use it as the
// route's result type: rockhandlers.Handler[*Params, *rockhttp.File].
type File struct {
	Name        string    // download name; sets Content-Disposition when Download is true
	ContentType string    // default: detected from Name, else application/octet-stream
	Content     io.Reader // closed after sending if it is an io.Closer
	ModTime     time.Time // enables If-Modified-Since when Content is an io.ReadSeeker
	Download    bool      // ask the browser to save instead of display
}

// Redirect is a result that redirects. Use it as the handler's Out.
type Redirect struct {
	URL  string
	Code int // default 302 Found
}

// --- request-scoped access, for what the typed result can't express ---

type exchangeKey struct{}

// exchange holds the raw request and response extras for one call.
type exchange struct {
	req     *http.Request
	header  http.Header
	cookies []*http.Cookie
}

func withExchange(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, exchangeKey{}, &exchange{req: r, header: http.Header{}})
}

func exchangeFrom(ctx context.Context) *exchange {
	x, _ := ctx.Value(exchangeKey{}).(*exchange)
	return x
}

// Request returns the raw *http.Request of the current call, or nil outside
// an HTTP call. An escape hatch: prefer typed params.
func Request(ctx context.Context) *http.Request {
	if x := exchangeFrom(ctx); x != nil {
		return x.req
	}
	return nil
}

// SetHeader sets a response header. Ignored outside an HTTP call.
func SetHeader(ctx context.Context, key, value string) {
	if x := exchangeFrom(ctx); x != nil {
		x.header.Set(key, value)
	}
}

// SetCookie adds a Set-Cookie to the response. Ignored outside an HTTP call.
func SetCookie(ctx context.Context, c *http.Cookie) {
	if x := exchangeFrom(ctx); x != nil {
		x.cookies = append(x.cookies, c)
	}
}

// applyExtras writes headers and cookies set by the handler.
func applyExtras(ctx context.Context, w http.ResponseWriter) {
	x := exchangeFrom(ctx)
	if x == nil {
		return
	}
	for k, vs := range x.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	for _, c := range x.cookies {
		http.SetCookie(w, c)
	}
}
