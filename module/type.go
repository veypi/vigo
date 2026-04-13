//
// type.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package module

import "github.com/veypi/vigo"

type route[T, U any] struct {
	Path       string
	Method     string
	Descrition string
	Handler    func(*vigo.X, T) (U, error)
}
