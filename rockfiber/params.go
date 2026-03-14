package rockfiber

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"

	"github.com/gofiber/fiber/v2"
)

// Params is a constraint for request parameter structs that validate themselves.
type Params[T any] interface {
	*T
	Validate(ctx *fiber.Ctx) error
}

// DefaultHandler parses the request into a P, validates it, and stores the result
// in ctx.Locals under the given key (default: "params").
// Use GetFromContext to retrieve it in downstream handlers.
func DefaultHandler[T any, P Params[T]](key ...string) fiber.Handler {
	ctxKey := "params"
	if len(key) > 0 && key[0] != "" {
		ctxKey = key[0]
	}
	return func(ctx *fiber.Ctx) error {
		var p P = new(T)

		if err := ParseRequest(ctx, p); err != nil {
			return NewError(http.StatusBadRequest, err.Error())
		}

		if err := p.Validate(ctx); err != nil {
			var errResp *ErrorResponse
			if errors.As(err, &errResp) {
				return errResp
			}
			return NewError(http.StatusBadRequest, err.Error())
		}

		ctx.Locals(ctxKey, p)
		return ctx.Next()
	}
}

// ParseRequest populates params from the incoming request.
//
// Supported struct tags:
//
//	reqHeader:"name"  — request header
//	query:"name"      — query parameter
//	params:"name"     — URL path parameter
//	json/xml/form     — request body (non-GET only)
//	form:"name"       — multipart file (*multipart.FileHeader or []*multipart.FileHeader)
//
// Parsing order: query → body → headers → path params.
// Path params are applied last and will overwrite earlier values for the same field.
func ParseRequest(ctx *fiber.Ctx, params interface{}) error {
	if params == nil {
		return fmt.Errorf("params must not be nil")
	}
	if reflect.TypeOf(params).Kind() != reflect.Ptr {
		return fmt.Errorf("params must be a pointer")
	}

	if err := ctx.QueryParser(params); err != nil {
		return fmt.Errorf("failed to parse query: %w", err)
	}

	if ctx.Method() != http.MethodGet && len(ctx.Body()) > 0 {
		if err := ctx.BodyParser(params); err != nil {
			return fmt.Errorf("failed to parse body: %w", err)
		}
		if err := parseMultipartFiles(ctx, params); err != nil {
			return fmt.Errorf("failed to parse multipart files: %w", err)
		}
	}

	if err := ctx.ReqHeaderParser(params); err != nil {
		return fmt.Errorf("failed to parse headers: %w", err)
	}

	if err := ctx.ParamsParser(params); err != nil {
		return fmt.Errorf("failed to parse path params: %w", err)
	}

	return nil
}

var (
	fileHeaderType  = reflect.TypeOf((*multipart.FileHeader)(nil))    // *multipart.FileHeader
	fileHeaderSlice = reflect.TypeOf([]*multipart.FileHeader(nil))    // []*multipart.FileHeader
)

// parseMultipartFiles reads file fields from a multipart form into the struct.
// Fields tagged with `form:"name"` of type *multipart.FileHeader receive the first file.
// Fields of type []*multipart.FileHeader receive all files for that key.
// If the request is not multipart, this is a no-op.
func parseMultipartFiles(ctx *fiber.Ctx, out interface{}) error {
	form, err := ctx.MultipartForm()
	if err != nil {
		// Not a multipart request — nothing to do.
		return nil
	}

	val := reflect.ValueOf(out)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return fmt.Errorf("out must be a non-nil pointer")
	}
	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return fmt.Errorf("out must point to a struct")
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("form")
		if tag == "" {
			continue
		}
		field := val.Field(i)
		if !field.CanSet() {
			continue
		}
		files := form.File[tag]
		switch field.Type() {
		case fileHeaderType:
			if len(files) > 0 {
				field.Set(reflect.ValueOf(files[0]))
			}
		case fileHeaderSlice:
			field.Set(reflect.ValueOf(files))
		}
	}
	return nil
}
