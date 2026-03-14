//
// types.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package vigo

import (
	"net/http"
	"reflect"
	_ "unsafe"

	"github.com/veypi/vigo/contrib/event"
)

//go:noescape
//go:linkname nanotime runtime.nanotime
func nanotime() int64

var Event = event.Default

type Middleware func(x *X) error

type FuncStandard[T, U any] func(*X, T) (U, error)

// handlers
type FuncX2AnyErr = func(*X) (any, error)

type FuncErr = func(*X, error) error

func IgnoreErr(x *X, err error) error {
	return nil
}

type FuncSkipBefore func()

var (
	SkipBefore FuncSkipBefore = func() {}
	Stop                      = func(x *X) {
		x.Stop()
	}
)

func TryStandardize(fn any) (func(*X) (any, error), bool) {
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return nil, false
	}
	numIn := fnType.NumIn()
	numOut := fnType.NumOut()

	if numIn > 2 || numOut > 2 {
		return nil, false
	}

	// Case 1: Standard func(*X) (any, error)
	if f, ok := fn.(FuncX2AnyErr); ok {
		return f, true
	}
	if f, ok := fn.(func(*X)); ok {
		return func(x *X) (any, error) {
			f(x)
			return nil, nil
		}, true
	}
	if f, ok := fn.(func(*X) error); ok {
		return func(x *X) (any, error) {
			return nil, f(x)
		}, true
	}
	if f, ok := fn.(func(*X) any); ok {
		return func(x *X) (any, error) {
			return f(x), nil
		}, true
	}
	if f, ok := fn.(func(*X, any)); ok {
		return func(x *X) (any, error) {
			f(x, x.PipeValue)
			return nil, nil
		}, true
	}
	if f, ok := fn.(func(*X, any) error); ok {
		return func(x *X) (any, error) {
			return nil, f(x, x.PipeValue)
		}, true
	}
	if f, ok := fn.(func(*X, any) any); ok {
		return func(x *X) (any, error) {
			return f(x, x.PipeValue), nil
		}, true
	}
	if f, ok := fn.(func(*X, any) (any, error)); ok {
		return func(x *X) (any, error) {
			return f(x, x.PipeValue)
		}, true
	}

	// Case 2: HTTP Handler func(http.ResponseWriter, *http.Request) ...
	if numIn == 2 && fnType.In(0) == reflect.TypeOf((*http.ResponseWriter)(nil)).Elem() && fnType.In(1) == reflect.TypeOf((*http.Request)(nil)) {
		// Optimization: Use direct type assertion if possible, otherwise use reflection (slow path)
		// But here we can use a closure with type assertion if we know the signature matches.
		// However, we can't assert to `http.HandlerFunc` if it's not that type.
		// We can assert to `func(http.ResponseWriter, *http.Request)`
		if f, ok := fn.(func(http.ResponseWriter, *http.Request)); ok {
			return func(x *X) (any, error) {
				f(x.ResponseWriter(), x.Request)
				return nil, nil
			}, true
		}
		if f, ok := fn.(func(http.ResponseWriter, *http.Request) error); ok {
			return func(x *X) (any, error) {
				return nil, f(x.ResponseWriter(), x.Request)
			}, true
		}

		// Fallback to reflection for HTTP handler if it has return values (unlikely for standard signature)
		// The standard signature is void return.
		// If it has return values, it's not a standard HTTP handler.
		// Our check above `numIn == 2` doesn't check return values.
		// Let's assume if it matches inputs, it's HTTP handler intent.

		fnValue := reflect.ValueOf(fn)
		resultHandler := makeResultHandler(fnType)
		return func(x *X) (any, error) {
			args := [2]reflect.Value{
				reflect.ValueOf(x.ResponseWriter()),
				reflect.ValueOf(x.Request),
			}
			return resultHandler(fnValue.Call(args[:]))
		}, true
	}

	// Case 3: func(*X, [T]) ...
	if numIn > 0 && fnType.In(0) == reflect.TypeOf((*X)(nil)) {
		if optimized, ok := createTypedRequestFunc(fn, fnType); ok {
			return optimized, true
		}
		return createStandardizedFunc(fn, fnType), true
	}

	return nil, false
}

// Standardize converts a function to a standard handler using reflection or unsafe optimization.
func Standardize[T any](handler T) FuncX2AnyErr {
	// 1. If it's already a standard handler, return it directly
	if h, ok := any(handler).(FuncX2AnyErr); ok {
		return h
	}

	// 2. Try to standardize
	if std, ok := TryStandardize(handler); ok {
		return std
	}

	// 3. Panic if unsupported (initialization time check)
	panic("vigo: handler function signature not supported: " + reflect.TypeOf(handler).String())
}

func makeResultHandler(fnType reflect.Type) func([]reflect.Value) (any, error) {
	numOut := fnType.NumOut()
	if numOut == 0 {
		return func([]reflect.Value) (any, error) { return nil, nil }
	}
	if numOut == 1 {
		// Check if output is error
		if fnType.Out(0).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			return func(res []reflect.Value) (any, error) {
				if res[0].IsNil() {
					return nil, nil
				}
				return nil, res[0].Interface().(error)
			}
		}
		return func(res []reflect.Value) (any, error) {
			return res[0].Interface(), nil
		}
	}
	// numOut == 2
	return func(res []reflect.Value) (any, error) {
		var err error
		if !res[1].IsNil() {
			err = res[1].Interface().(error)
		}
		return res[0].Interface(), err
	}
}

func createTypedRequestFunc(originalFn any, fnType reflect.Type) (func(*X) (any, error), bool) {
	if fnType.NumIn() != 2 || fnType.In(0) != reflect.TypeOf((*X)(nil)) {
		return nil, false
	}

	reqType := fnType.In(1)
	if reqType.Kind() != reflect.Ptr {
		return nil, false
	}
	return nil, false
}

// 使用反射创建规则化的函数
func createStandardizedFunc(originalFn any, fnType reflect.Type) func(*X) (any, error) {
	fnValue := reflect.ValueOf(originalFn)
	numIn := fnType.NumIn()
	resultHandler := makeResultHandler(fnType)

	if numIn == 1 {
		return func(x *X) (any, error) {
			args := [1]reflect.Value{reflect.ValueOf(x)}
			return resultHandler(fnValue.Call(args[:]))
		}
	}

	tType := fnType.In(1)
	if tType.Kind() == reflect.Interface && tType.NumMethod() == 0 {
		return func(x *X) (any, error) {
			var pipeValue reflect.Value
			if x.PipeValue == nil {
				pipeValue = reflect.Zero(tType)
			} else {
				pipeValue = reflect.ValueOf(x.PipeValue)
			}
			args := [2]reflect.Value{reflect.ValueOf(x), pipeValue}
			return resultHandler(fnValue.Call(args[:]))
		}
	}

	isPtr := tType.Kind() == reflect.Ptr
	elemType := tType
	if isPtr {
		elemType = tType.Elem()
	}

	return func(x *X) (any, error) {
		val := reflect.New(elemType)
		if err := x.Parse(val.Interface()); err != nil {
			return nil, err
		}
		arg1 := val.Elem()
		if isPtr {
			arg1 = val
		}
		args := [2]reflect.Value{reflect.ValueOf(x), arg1}
		return resultHandler(fnValue.Call(args[:]))
	}
}
