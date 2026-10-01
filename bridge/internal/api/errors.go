package api

import (
	"errors"
	"net/http"
)

// Error is `{"error": {"code": "...", "message": "..."}}` with the matching status.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func NotFound(m string) *Error   { return NewError(http.StatusNotFound, "not_found", m) }
func BadRequest(m string) *Error { return NewError(http.StatusBadRequest, "bad_request", m) }

var Unauthorized = NewError(http.StatusUnauthorized, "unauthorized", "missing or invalid token")

// ErrorBody is the JSON shape of an error response.
type ErrorBody struct {
	Error ErrorInner `json:"error"`
}

type ErrorInner struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Body() ErrorBody {
	return ErrorBody{Error: ErrorInner{Code: e.Code, Message: e.Message}}
}

// APIErrorer is implemented by errors that know their HTTP mapping (herdr.Error does).
type APIErrorer interface {
	APIError() *Error
}

// FromError maps any error to an *Error: an *Error as is, an APIErrorer (herdr failures)
// through its mapping, and anything else to 500 internal.
func FromError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var m APIErrorer
	if errors.As(err, &m) {
		return m.APIError()
	}
	return NewError(http.StatusInternalServerError, "internal", err.Error())
}
