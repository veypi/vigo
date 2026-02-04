//
// cors.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-12-06 15:56
// Distributed under terms of the MIT license.
//

package cors

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/veypi/vigo"
)

// IsCrossOrigin 完整的跨域判断
func IsCrossOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// 获取当前请求的协议
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	// 获取端口
	requestPort := getPort(r.Host, scheme)
	originPort := getPort(originURL.Host, originURL.Scheme)

	// 比较协议、主机、端口
	return scheme != originURL.Scheme ||
		getHost(r.Host) != getHost(originURL.Host) ||
		requestPort != originPort
}

// getHost 提取主机名
func getHost(host string) string {
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		return host[:colonIndex]
	}
	return host
}

// getPort 获取端口号
func getPort(host, scheme string) string {
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		return host[colonIndex+1:]
	}
	// 返回默认端口
	if scheme == "https" {
		return "443"
	}
	return "80"
}

func AllowAny(x *vigo.X) {
	if IsCrossOrigin(x.Request) {
		origin := x.Request.Header.Get("Origin")
		x.Header().Set("Access-Control-Allow-Origin", origin)
		x.Header().Set("Access-Control-Allow-Credentials", "true")
		x.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, PATCH, PROPFIND")
		x.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, depth")
		x.Header().Set("Access-Control-Expose-Headers", "vigo-scoped,vigo-vdev")
		if x.Request.Method == http.MethodOptions && x.Request.Header.Get("Access-Control-Request-Method") != "" {
			x.Stop()
		}
	}
}

func CorsAllow(domains ...string) func(x *vigo.X) {
	return func(x *vigo.X) {
		if IsCrossOrigin(x.Request) {
			origin := x.Request.Header.Get("Origin")
			if slices.Contains(domains, origin) {
				x.Header().Set("Access-Control-Allow-Origin", origin)
				x.Header().Set("Access-Control-Allow-Credentials", "true")
				x.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, PATCH")
				x.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
				if x.Request.Method == http.MethodOptions && x.Request.Header.Get("Access-Control-Request-Method") != "" {
					x.Stop()
				}
				return
			}
		}
	}
}
