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
	"unsafe"
)

//go:noescape
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// map
type M = map[string]any

// slice
type S = []any

type FuncStandard[T, U any] func(*X, T) (U, error)

// handlers
type FuncX2AnyErr = func(*X) (any, error)

type FuncErr = func(*X, error) error

func IgnoreErr(x *X, err error) error {
	return nil
}

type FuncSkipBefore func()

var SkipBefore FuncSkipBefore = func() {}

func DiliverData(x *X, data any) (any, error) {
	return data, nil
}

var xType = reflect.TypeOf((*X)(nil))

type eface struct {
	_type unsafe.Pointer
	data  unsafe.Pointer
}

func getRType(t reflect.Type) unsafe.Pointer {
	return (*eface)(unsafe.Pointer(&t)).data
}

func packEface(typ unsafe.Pointer, data unsafe.Pointer) any {
	return *(*any)(unsafe.Pointer(&eface{typ, data}))
}

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

		// Fallback to reflection for HTTP handler if it has return values (unlikely for standard signature)
		// The standard signature is void return.
		// If it has return values, it's not a standard HTTP handler.
		// Our check above `numIn == 2` doesn't check return values.
		// Let's assume if it matches inputs, it's HTTP handler intent.

		fnValue := reflect.ValueOf(fn)
		resultHandler := makeResultHandler(fnType)
		return func(x *X) (any, error) {
			args := []reflect.Value{
				reflect.ValueOf(x.ResponseWriter()),
				reflect.ValueOf(x.Request),
			}
			return resultHandler(fnValue.Call(args))
		}, true
	}

	// Case 3: func(*X, [T]) ...
	if numIn > 0 && fnType.In(0) == reflect.TypeOf((*X)(nil)) {
		// Try optimization with unsafe casting for common pointer patterns
		if optimized, ok := tryOptimizeUnsafe(fn, fnType); ok {
			return optimized, true
		}
		// Fallback to reflection
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

func tryOptimizeUnsafe(fn any, fnType reflect.Type) (func(*X) (any, error), bool) {
	numIn := fnType.NumIn()
	numOut := fnType.NumOut()

	// Helper types for unsafe casting
	type FuncIn1RetErr func(*X) error
	type FuncIn1RetPtr func(*X) (unsafe.Pointer, error)
	type FuncIn1RetAny func(*X) (any, error)

	type FuncIn2PtrRetErr func(*X, unsafe.Pointer) error
	type FuncIn2PtrRetPtr func(*X, unsafe.Pointer) (unsafe.Pointer, error)
	type FuncIn2PtrRetAny func(*X, unsafe.Pointer) (any, error)

	type FuncIn2AnyRetErr func(*X, any) error
	type FuncIn2AnyRetPtr func(*X, any) (unsafe.Pointer, error)
	type FuncIn2AnyRetAny func(*X, any) (any, error)

	// Get function pointer
	fnPtr := (*eface)(unsafe.Pointer(&fn)).data

	// 1. Single Input: func(*X)
	if numIn == 1 {
		if numOut == 1 && fnType.Out(0) == reflect.TypeOf((*error)(nil)).Elem() {
			// func(*X) error
			casted := *(*FuncIn1RetErr)(unsafe.Pointer(&fnPtr))
			return func(x *X) (any, error) {
				return nil, casted(x)
			}, true
		}
		if numOut == 2 && fnType.Out(1) == reflect.TypeOf((*error)(nil)).Elem() {
			out0 := fnType.Out(0)
			if out0.Kind() == reflect.Ptr {
				// func(*X) (*R, error)
				casted := *(*FuncIn1RetPtr)(unsafe.Pointer(&fnPtr))
				out0Typ := getRType(out0)
				return func(x *X) (any, error) {
					res, err := casted(x)
					if err != nil {
						return nil, err
					}
					// Wrap pointer in interface
					return packEface(out0Typ, res), nil
				}, true
			}
			if out0.Kind() == reflect.Interface {
				// func(*X) (any, error)
				// Since FuncX2AnyErr is already checked, this handles other interface returns
				casted := *(*FuncIn1RetAny)(unsafe.Pointer(&fnPtr))
				return func(x *X) (any, error) {
					return casted(x)
				}, true
			}
		}
	}

	// 2. Two Inputs: func(*X, T)
	if numIn == 2 {
		in1 := fnType.In(1)

		// 2a. Input is Pointer: func(*X, *T)
		if in1.Kind() == reflect.Ptr {
			elemType := in1.Elem()
			// Prepare allocator
			// We need to allocate *T.
			// reflect.New(elemType) returns Value wrapping *T.
			// We can get UnsafePointer from it.

			// Check output
			if numOut == 1 && fnType.Out(0) == reflect.TypeOf((*error)(nil)).Elem() {
				// func(*X, *T) error
				casted := *(*FuncIn2PtrRetErr)(unsafe.Pointer(&fnPtr))
				return func(x *X) (any, error) {
					// Allocate T
					val := reflect.New(elemType)
					ptr := val.UnsafePointer()
					// Parse into T
					if err := x.Parse(val.Interface()); err != nil {
						return nil, err
					}
					return nil, casted(x, ptr)
				}, true
			}

			if numOut == 2 && fnType.Out(1) == reflect.TypeOf((*error)(nil)).Elem() {
				out0 := fnType.Out(0)
				if out0.Kind() == reflect.Ptr {
					// func(*X, *T) (*R, error)
					casted := *(*FuncIn2PtrRetPtr)(unsafe.Pointer(&fnPtr))
					out0Typ := getRType(out0)
					return func(x *X) (any, error) {
						val := reflect.New(elemType)
						ptr := val.UnsafePointer()
						if err := x.Parse(val.Interface()); err != nil {
							return nil, err
						}
						res, err := casted(x, ptr)
						if err != nil {
							return nil, err
						}
						return packEface(out0Typ, res), nil
					}, true
				}
				if out0.Kind() == reflect.Interface {
					// func(*X, *T) (any, error)
					casted := *(*FuncIn2PtrRetAny)(unsafe.Pointer(&fnPtr))
					return func(x *X) (any, error) {
						val := reflect.New(elemType)
						ptr := val.UnsafePointer()
						if err := x.Parse(val.Interface()); err != nil {
							return nil, err
						}
						return casted(x, ptr)
					}, true
				}
			}
		}

		// 2b. Input is Interface (PipeValue): func(*X, any)
		if in1.Kind() == reflect.Interface && in1.NumMethod() == 0 {
			// func(*X, any) ...
			// PipeValue handling

			if numOut == 1 && fnType.Out(0) == reflect.TypeOf((*error)(nil)).Elem() {
				// func(*X, any) error
				casted := *(*FuncIn2AnyRetErr)(unsafe.Pointer(&fnPtr))
				return func(x *X) (any, error) {
					return nil, casted(x, x.PipeValue)
				}, true
			}

			if numOut == 2 && fnType.Out(1) == reflect.TypeOf((*error)(nil)).Elem() {
				out0 := fnType.Out(0)
				if out0.Kind() == reflect.Ptr {
					// func(*X, any) (*R, error)
					casted := *(*FuncIn2AnyRetPtr)(unsafe.Pointer(&fnPtr))
					out0Typ := getRType(out0)
					return func(x *X) (any, error) {
						res, err := casted(x, x.PipeValue)
						if err != nil {
							return nil, err
						}
						return packEface(out0Typ, res), nil
					}, true
				}
				if out0.Kind() == reflect.Interface {
					// func(*X, any) (any, error)
					casted := *(*FuncIn2AnyRetAny)(unsafe.Pointer(&fnPtr))
					return func(x *X) (any, error) {
						return casted(x, x.PipeValue)
					}, true
				}
			}
		}
	}

	return nil, false
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

// 使用反射创建规则化的函数
func createStandardizedFunc(originalFn any, fnType reflect.Type) func(*X) (any, error) {
	fnValue := reflect.ValueOf(originalFn)
	numIn := fnType.NumIn()

	// Pre-calculate argument generators
	var argGenerators []func(*X) (reflect.Value, error)

	// Arg 0 is always *X
	argGenerators = append(argGenerators, func(x *X) (reflect.Value, error) {
		return reflect.ValueOf(x), nil
	})

	if numIn == 2 {
		tType := fnType.In(1)
		if tType.Kind() == reflect.Interface && tType.NumMethod() == 0 {
			// PipeValue
			argGenerators = append(argGenerators, func(x *X) (reflect.Value, error) {
				if x.PipeValue == nil {
					return reflect.Zero(tType), nil
				}
				return reflect.ValueOf(x.PipeValue), nil
			})
		} else {
			// Request Object
			isPtr := tType.Kind() == reflect.Ptr
			var elemType reflect.Type
			if isPtr {
				elemType = tType.Elem()
			} else {
				elemType = tType
			}
			argGenerators = append(argGenerators, func(x *X) (reflect.Value, error) {
				val := reflect.New(elemType)
				if err := x.Parse(val.Interface()); err != nil {
					return reflect.Value{}, err
				}
				if isPtr {
					return val, nil
				}
				return val.Elem(), nil
			})
		}
	}

	resultHandler := makeResultHandler(fnType)

	return func(x *X) (any, error) {
		args := make([]reflect.Value, len(argGenerators))
		for i, gen := range argGenerators {
			val, err := gen(x)
			if err != nil {
				return nil, err
			}
			args[i] = val
		}
		return resultHandler(fnValue.Call(args))
	}
}
