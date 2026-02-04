//
// proxy.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package proxy

import (
	"net/http/httputil"
	"net/url"

	"github.com/veypi/vigo"
)

func ProxyTo(targetHost string) func(*vigo.X) {
	targetURL, err := url.Parse(targetHost)
	if err != nil {
		panic("Invalid target URL: " + targetHost)
	}
	return func(x *vigo.X) {
		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		originalPath := x.PathParams.Get("path")
		r := x.Request
		r.URL.Path = r.URL.Path + originalPath
		if r.URL.RawPath != "" {
			r.URL.RawPath = r.URL.Path
		}
		r.Host = targetURL.Host

		proxy.ServeHTTP(x.ResponseWriter(), r)
	}
}
