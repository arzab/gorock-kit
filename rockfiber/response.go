package rockfiber

import (
	"fmt"
	"net/http"
)

// ErrorResponse is the standard error payload returned by all API error responses.
// It implements the error interface, so it can be returned directly from fiber handlers.
type ErrorResponse struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	Action  string `json:"action,omitempty"`
}

func (e *ErrorResponse) Error() string {
	if e.Source == "" {
		return fmt.Sprintf("(%d) - %s", e.Code, e.Message)
	}
	if e.Action == "" {
		return fmt.Sprintf("(%d) - [%s] - %s", e.Code, e.Source, e.Message)
	}
	return fmt.Sprintf("(%d) - [%s-%s] - %s", e.Code, e.Source, e.Action, e.Message)
}

// NewError creates an ErrorResponse with the given HTTP status code and message.
func NewError(statusCode int, message string) *ErrorResponse {
	return &ErrorResponse{
		Code:    statusCode,
		Status:  http.StatusText(statusCode),
		Message: message,
	}
}

// WithSource sets the Source field and returns the receiver for chaining.
func (e *ErrorResponse) WithSource(source string) *ErrorResponse {
	e.Source = source
	return e
}

// WithAction sets the Action field and returns the receiver for chaining.
func (e *ErrorResponse) WithAction(action string) *ErrorResponse {
	e.Action = action
	return e
}
