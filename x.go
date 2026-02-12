//
// x.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-08-09 13:08
// Distributed under terms of the MIT license.
//

package vigo

import (
	"context"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/veypi/vigo/logv"
)

const version = "v0.6.0"

type Param struct {
	Key   string
	Value string
}

type PathParams []Param

func (p PathParams) Get(key string) string {
	v, _ := p.Try(key)
	return v
}

func (p PathParams) Try(key string) (string, bool) {
	for _, entry := range p {
		if entry.Key == key {
			return entry.Value, true
		}
	}
	return "", false
}

// X is the context for the request
type X struct {
	writer     http.ResponseWriter
	Request    *http.Request
	PathParams PathParams
	routeVars  map[string]any
	fcs        []any
	fcsInfo    []*HandlerInfo
	fid        int
	PipeValue  any
}

var _ http.ResponseWriter = &X{}

func (x *X) Stop() {
	x.fid = 99999999
}

func (x *X) Skip(counts ...uint) {
	count := 1
	if len(counts) > 0 {
		count = int(counts[0])
	}
	x.fid += int(count)
}

func (x *X) Next() {
	for {
		// args[0] vaild
		var err error

		if x.fid >= len(x.fcs) {
			return
		}
		idx := x.fid
		fc := x.fcs[idx]
		x.fid++

		switch fc := fc.(type) {
		case FuncX2AnyErr:
			x.PipeValue, err = fc(x)
		case FuncErr:
		default:
			logv.Warn().Msgf("unknown func type %T", fc)
		}

		if err != nil {
			if !x.handleErr(err) {
				name := ""
				if len(x.fcsInfo) > idx {
					name = x.fcsInfo[idx].Name
				}
				if name == "" {
					name = runtime.FuncForPC(reflect.ValueOf(fc).Pointer()).Name()
				}
				logv.WithNoCaller.Warn().Msgf("unhandled error in %s: %v", name, err)
			}
			return
		}
	}
}

func (x *X) handleErr(err error) bool {
	if x.fid >= len(x.fcs) {
		return false
	}
	for x.fid < len(x.fcs) {
		fc, ok := x.fcs[x.fid].(FuncErr)
		x.fid++
		if ok {
			err = fc(x, err)
			if err == nil {
				return true
			}
		}
	}
	return false
}

func (x *X) ResponseWriter() http.ResponseWriter {
	return x.writer
}

func (x *X) Get(key string) any {
	v := x.Request.Context().Value(key)
	if v != nil {
		return v
	}
	if x.routeVars != nil {
		return x.routeVars[key]
	}
	return nil
}

func (x *X) Set(key string, value any) {
	if x.Request == nil {
		logv.Warn().Msgf("set %s=%v to nil request", key, value)
		return
	}
	x.Request = x.Request.WithContext(context.WithValue(x.Request.Context(), key, value))
}

func (x *X) Context() context.Context {
	return x.Request.Context()
}

func (x *X) GetRemoteIP() string {
	// 首先尝试从 X-Forwarded-For 获取 IP 地址
	ip := x.Request.Header.Get("X-Forwarded-For")
	if ip != "" {
		// X-Forwarded-For 可能包含多个 IP 地址，以逗号分隔，
		// 这里我们取第一个 IP 地址作为客户端的 IP。
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}

	// 如果 X-Forwarded-For 不存在，则尝试从 X-Real-IP 获取 IP 地址
	ip = x.Request.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}

	// 如果以上两个都没有，则直接从 RemoteAddr 获取 IP 地址
	ip, _, err := net.SplitHostPort(x.Request.RemoteAddr)
	if err != nil {
		return ""
	}
	return ip
}

var xPool = sync.Pool{
	New: func() any {
		return &X{
			PathParams: make(PathParams, 0, 10),
		}
	},
}

func acquire() *X {
	v := xPool.Get()
	return v.(*X)
}

func release(x *X) {
	x.fid = 0
	// 显式清理 slice 底层数组引用的字符串
	// 否则底层的 Param 结构体依然持有字符串引用，阻碍 GC
	for i := range x.PathParams {
		x.PathParams[i] = Param{}
	}
	x.PathParams = x.PathParams[:0]
	x.Request = nil
	x.writer = nil
	x.routeVars = nil
	x.fcs = nil
	x.PipeValue = nil
	xPool.Put(x)
}
