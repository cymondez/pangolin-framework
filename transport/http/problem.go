package http

import (
	"net/http"
	"strings"
)

// ProblemError is a standard HTTP-classified application error recognized by
// WriteError. Its HTTP status and machine-readable code are exposed safely in
// Problem Details responses while its underlying cause stays available for
// errors.Is and errors.As without leaking raw error text to clients.
type ProblemError struct {
	status int
	code   string
	cause  error
}

// NewProblemError wraps cause with an HTTP error status (400-599) and a safe
// machine-readable code. Out-of-range statuses default to 500 and empty codes
// default to "INTERNAL" so error responses always fail closed.
func NewProblemError(status int, code string, cause error) error {
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	code = strings.TrimSpace(code)
	if code == "" {
		code = "INTERNAL"
	}
	return &ProblemError{status: status, code: code, cause: cause}
}

func (e *ProblemError) Error() string {
	if e == nil || e.code == "" {
		return "INTERNAL"
	}
	return e.code
}

func (e *ProblemError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *ProblemError) HTTPStatus() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	return e.status
}

func (e *ProblemError) Code() string {
	if e == nil || e.code == "" {
		return "INTERNAL"
	}
	return e.code
}
