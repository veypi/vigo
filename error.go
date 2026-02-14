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
)

var (
	// 4xx 客户端错误
	// 400xx 参数相关错误
	ErrBadRequest     = NewError("bad request").WithCode(40000)
	ErrInvalidArg     = NewError("invalid arg").WithCode(40001)
	ErrMissingArg     = NewError("missing arg").WithCode(40002)
	ErrArgFormat      = NewError("arg format error").WithCode(40003)

	// 401xx 认证授权相关错误
	ErrUnauthorized   = NewError("unauthorized").WithCode(40100)  // 未登录/无token
	ErrTokenInvalid   = NewError("token invalid").WithCode(40101) // token无效
	ErrTokenExpired   = NewError("token expired").WithCode(40102) // token过期
	ErrNoPermission   = NewError("no permission").WithCode(40103) // 无操作权限
	ErrForbidden      = NewError("forbidden").WithCode(40300)     // 禁止访问

	// 404xx 资源不存在
	ErrNotFound       = NewError("not found").WithCode(40400)
	ErrResourceNotFound = NewError("resource not found").WithCode(40401)
	ErrEndpointNotFound = NewError("endpoint not found").WithCode(40402)

	// 409xx 资源冲突
	ErrConflict       = NewError("resource conflict").WithCode(40900)
	ErrAlreadyExists  = NewError("resource already exists").WithCode(40901)

	// 429xx 限流
	ErrTooManyRequests = NewError("too many requests").WithCode(42900)

	// 5xx 服务端错误
	// 500xx 内部错误
	ErrInternalServer = NewError("internal server error").WithCode(50000)
	ErrDatabase       = NewError("database error").WithCode(50001)
	ErrCache          = NewError("cache error").WithCode(50002)
	ErrThirdParty     = NewError("third party service error").WithCode(50003)

	// 501xx 功能相关
	ErrNotImplemented = NewError("not implemented").WithCode(50100)
	ErrNotSupported   = NewError("not supported").WithCode(50101)

	// 503xx 服务不可用
	ErrServiceUnavailable = NewError("service unavailable").WithCode(50300)

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
		Message: e.Message + ": " + fmt.Sprint(a...),
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
