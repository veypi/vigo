// Package common
// json.go
// Copyright (C) 2025 veypi <i@veypi.com>
// 2025-07-15 16:00
// Distributed under terms of the MIT license.
package common

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/veypi/vigo"
)

func JsonResponse(x *vigo.X, data any) error {
	if !x.WroteHeader() {
		x.WriteHeader(200)
	}
	switch v := data.(type) {
	case []byte:
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "application/octet-stream")
		}
		_, err := x.Write(v)
		return err
	case string:
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, err := x.WriteString(v)
		return err
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, err := fmt.Fprintf(x, "%v", v)
		return err
	}
	return x.JSON(data)
}

func JsonErrorResponse(x *vigo.X, err error) error {
	code := 400
	if e, ok := err.(*vigo.Error); ok {
		code = e.Code
		if code > 999 {
			code, _ = strconv.Atoi(strconv.Itoa(code)[:3])
		}
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		x.WriteHeader(code)
		resp := map[string]any{"code": e.Code, "message": e.Message}
		b, _ := json.Marshal(resp)
		_, err := x.Write(b)
		return err
	}
	if x.Header().Get("Content-Type") == "" {
		x.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	x.WriteHeader(code)
	resp := map[string]any{"code": code, "message": err.Error()}
	b, _ := json.Marshal(resp)
	_, err = x.Write(b)
	return err
}
