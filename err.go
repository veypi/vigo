//
// err.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-09-24 21:22
// Distributed under terms of the MIT license.
//

package vigo

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrCrash           = NewError("crash")
	ErrNotFound        = NewError("not found").WithCode(404)
	ErrArgMissing      = &Error{Code: http.StatusConflict, Message: "missing arg: %s"}
	ErrArgInvalid      = &Error{Code: http.StatusConflict, Message: "invalid arg: %s"}
	ErrNotImplemented  = NewError("not implemented")
	ErrNotAllowed      = NewError("not allowed")
	ErrNotSupported    = NewError("not supported")
	ErrNotAuthorized   = NewError("not authorized").WithCode(40101)
	ErrNotPermitted    = NewError("not permitted").WithCode(40102)
	ErrForbidden       = NewError("not forbidden").WithCode(http.StatusForbidden)
	ErrInternalServer  = NewError("internal server error").WithCode(500)
	ErrTooManyRequests = NewError("too many requests").WithCode(http.StatusTooManyRequests)
)

type Error struct {
	Code    int
	Message string
}

var _ error = &Error{}

func (e *Error) Error() string {
	return fmt.Sprintf("code: %d, message: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return errors.New(e.Message)
}

func (e *Error) WithCode(code int) *Error {
	e.Code = code
	return e
}

func (e *Error) WithArgs(a ...any) *Error {
	return &Error{
		Code:    e.Code,
		Message: fmt.Sprintf(e.Message, a...),
	}
}

func (e *Error) WithString(a string) *Error {
	return &Error{
		Code:    e.Code,
		Message: e.Message + "\n" + a,
	}
}

func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		Code:    e.Code,
		Message: msg,
	}
}

func (e *Error) WithError(err error) *Error {
	return &Error{
		Code:    e.Code,
		Message: e.Message + "\n" + err.Error(),
	}
}

func NewError(msg string, a ...any) *Error {
	e := &Error{
		Code:    400,
		Message: msg,
	}
	if len(a) > 0 {
		e.Message = fmt.Sprintf(msg, a...)
	}
	return e
}
