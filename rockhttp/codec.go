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

	"github.com/arzab/gorock-kit/rockdelivery"
)

// codec is the rockdelivery.Codec for net/http.
type codec struct {
	maxBody int64
}

func (c codec) Decode(_ context.Context, _ rockdelivery.Route, r *http.Request, in any) error {
	return bind(r, in, c.maxBody)
}

func (c codec) Encode(ctx context.Context, rt rockdelivery.Route, w http.ResponseWriter, out any) error {
	applyExtras(ctx, w)
	switch v := out.(type) {
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

	status := rockdelivery.MetaOr(rt, metaStatus, 0)
	if isNil(out) {
		if status == 0 {
			status = http.StatusNoContent
		}
		w.WriteHeader(status)
		return nil
	}
	if status == 0 {
		status = http.StatusOK
	}
	return writeJSON(w, status, out)
}

// ErrorBody is the JSON body of every error response.
type ErrorBody struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (c codec) EncodeError(ctx context.Context, _ rockdelivery.Route, w http.ResponseWriter, f rockdelivery.Failure) error {
	applyExtras(ctx, w)
	code := StatusOf(f.Kind)
	return writeJSON(w, code, ErrorBody{Code: code, Status: http.StatusText(code), Kind: f.Kind.String(), Message: f.Message})
}

// StatusOf maps an error kind to an HTTP status.
func StatusOf(k rockdelivery.Kind) int {
	switch k {
	case rockdelivery.KindInvalid:
		return http.StatusBadRequest
	case rockdelivery.KindUnauthorized:
		return http.StatusUnauthorized
	case rockdelivery.KindForbidden:
		return http.StatusForbidden
	case rockdelivery.KindNotFound:
		return http.StatusNotFound
	case rockdelivery.KindConflict:
		return http.StatusConflict
	case rockdelivery.KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err) // nothing written yet: the pipeline reports it
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

func writeFile(w http.ResponseWriter, r *http.Request, f *File) error {
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

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	}
	return false
}
