package rockfiber

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// GetFromContext retrieves a *T stored under keyStr in ctx.Locals.
// Returns an error if the key is missing or the stored value has the wrong type.
func GetFromContext[T any](ctx *fiber.Ctx, keyStr string) (*T, error) {
	obj := ctx.Locals(keyStr)
	if obj == nil {
		return nil, fmt.Errorf("ctx: no value found for key %q", keyStr)
	}
	result, ok := obj.(*T)
	if !ok {
		return nil, fmt.Errorf("ctx: value for key %q is %T, expected %T", keyStr, obj, (*T)(nil))
	}
	return result, nil
}

// HandlerInitInCtx returns a middleware that initialises a zero-value *T in ctx.Locals
// under keyStr. Useful for passing objects between middleware layers.
func HandlerInitInCtx[T any](keyStr string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		ctx.Locals(keyStr, new(T))
		return ctx.Next()
	}
}
