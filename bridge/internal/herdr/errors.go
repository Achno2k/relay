// Package herdr talks to herdr's unix socket: one JSON request per line, one response per line.
package herdr

import (
	"net/http"
	"strings"

	"relay/internal/api"
)

type ErrorKind int

const (
	ErrUnavailable ErrorKind = iota // can't connect
	ErrIO                           // connection broke
	ErrTimeout                      // no answer in time
	ErrRemote                       // herdr answered with an error
	ErrDecoding                     // unexpected response
)

// Error is Swift's HerdrError.
type Error struct {
	Kind    ErrorKind
	Code    string // ErrRemote only
	Message string
}

func (e *Error) Error() string {
	switch e.Kind {
	case ErrTimeout:
		return "herdr did not answer in time"
	case ErrRemote:
		return e.Code + ": " + e.Message
	}
	return e.Message
}

func Unavailable(m string) *Error  { return &Error{Kind: ErrUnavailable, Message: m} }
func IOError(m string) *Error      { return &Error{Kind: ErrIO, Message: m} }
func Timeout() *Error              { return &Error{Kind: ErrTimeout} }
func Remote(code, m string) *Error { return &Error{Kind: ErrRemote, Code: code, Message: m} }
func Decoding(m string) *Error     { return &Error{Kind: ErrDecoding, Message: m} }

// APIError maps herdr failures onto HTTP (api.FromError uses it).
func (e *Error) APIError() *api.Error {
	switch e.Kind {
	case ErrRemote:
		switch {
		case strings.Contains(e.Code, "not_found"):
			return api.NewError(http.StatusNotFound, "not_found", e.Message)
		case e.Code == "agent_blocked":
			return api.NewError(http.StatusConflict, "agent_blocked", e.Message)
		case strings.HasSuffix(e.Code, "_taken"): // agent_name_taken (QA R8-5)
			return api.NewError(http.StatusConflict, e.Code, e.Message)
		case strings.HasPrefix(e.Code, "unsupported"): // unsupported_agent_kind (QA R8-5)
			return api.NewError(http.StatusBadRequest, "unsupported", e.Message)
		case strings.HasPrefix(e.Code, "invalid"):
			return api.NewError(http.StatusBadRequest, e.Code, e.Message)
		}
		return api.NewError(http.StatusBadGateway, e.Code, e.Message)
	case ErrUnavailable:
		return api.NewError(http.StatusServiceUnavailable, "herdr_unavailable", e.Message)
	case ErrTimeout:
		return api.NewError(http.StatusGatewayTimeout, "herdr_timeout", e.Error())
	}
	return api.NewError(http.StatusBadGateway, "herdr_error", e.Message)
}
