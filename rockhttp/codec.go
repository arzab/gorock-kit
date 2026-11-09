package rockhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"reflect"

	"github.com/arzab/gorock-kit/rockhandlers"
)

// StatusClientClosedRequest is used when the request context was cancelled
// (the client went away): nothing is sent, but logs and proxies see 499.
const StatusClientClosedRequest = 499

// StatusOf maps an error kind to an HTTP status.
func StatusOf(k rockhandlers.Kind) int {
	switch k {
	case rockhandlers.KindInvalid:
		return http.StatusBadRequest
	case rockhandlers.KindUnauthorized:
		return http.StatusUnauthorized
	case rockhandlers.KindForbidden:
		return http.StatusForbidden
	case rockhandlers.KindNotFound:
		return http.StatusNotFound
	case rockhandlers.KindConflict:
		return http.StatusConflict
	case rockhandlers.KindUnavailable:
		return http.StatusServiceUnavailable
	case rockhandlers.KindCanceled:
		return StatusClientClosedRequest
	case rockhandlers.KindTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// ErrorBody is the JSON body of every error response.
type ErrorBody struct {
	Code    int               `json:"code"`
	Status  string            `json:"status"`
	Kind    string            `json:"kind"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (a *App) writeError(ctx context.Context, w http.ResponseWriter, err error) {
	applyExtras(ctx, w)
	kind := rockhandlers.KindOf(err)
	code := StatusOf(kind)
	if kind == rockhandlers.KindCanceled {
		w.WriteHeader(code) // the client is gone: status for logs only
		return
	}
	msg := rockhandlers.MessageOf(err)
	switch {
	case kind == rockhandlers.KindInternal:
		msg = a.internalMessage()
	case msg == "":
		msg = http.StatusText(code)
	}
	body := ErrorBody{Code: code, Status: http.StatusText(code), Kind: kind.String(), Message: msg, Fields: rockhandlers.FieldsOf(err)}
	_ = writeJSON(w, code, body)
}

// writeResult turns the route result into a response.
func writeResult(ctx context.Context, w http.ResponseWriter, res any, status int) error {
	applyExtras(ctx, w)
	switch v := res.(type) {
	case *File:
		return writeFile(w, Request(ctx), v)
	case *Redirect:
		code := v.Code
		if code == 0 {
			code = http.StatusFound
		}
		http.Redirect(w, Request(ctx), v.URL, code)
		return nil
	}
	if reflect.TypeOf(res).Elem().NumField() == 0 { // empty result struct: no body
		if status == 0 {
			status = http.StatusNoContent
		}
		w.WriteHeader(status)
		return nil
	}
	if status == 0 {
		status = http.StatusOK
	}
	return writeJSON(w, status, res)
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return fmt.Errorf("encode JSON: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

func writeFile(w http.ResponseWriter, r *http.Request, f *File) error {
	if f.Content == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return fmt.Errorf("rockhttp: File.Content is nil")
	}
	if c, ok := f.Content.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}
	ct := f.ContentType
	if ct == "" {
		ct = mime.TypeByExtension(filepath.Ext(f.Name))
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if f.Download {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Name}))
	}
	if rs, ok := f.Content.(io.ReadSeeker); ok {
		http.ServeContent(w, r, f.Name, f.ModTime, rs) // ranges, If-Modified-Since
		return nil
	}
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, f.Content)
	return err
}
