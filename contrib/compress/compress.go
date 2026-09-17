//
// compress.go
// Copyright (C) 2026 veypi <i@veypi.com>
// Distributed under terms of the MIT license.
//

// Package compress provides a vigo middleware that gzip-compresses eligible
// HTTP responses (text/js/css/json/xml/svg …) and transparently passes
// everything else through untouched: already-encoded bodies, Range requests,
// protocol upgrades (WebSocket), HEAD requests, unknown-but-clearly-binary
// content types, known small bodies and bodyless status codes
// (1xx/204/205/304/206).
//
// Usage:
//
//	r.Use(compress.New().Handler)
package compress

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"

	"github.com/veypi/vigo"
)

// Compressor is the gzip middleware. Create it with New and register its
// Handler via Router.Use.
type Compressor struct {
	minSize int
	level   int
}

// Option customises a Compressor.
type Option func(*Compressor)

// WithMinSize skips responses whose known Content-Length is below n bytes
// (default 1024). Responses with unknown length are still compressed.
func WithMinSize(n int) Option {
	return func(c *Compressor) {
		if n >= 0 {
			c.minSize = n
		}
	}
}

// WithLevel sets the gzip compression level (gzip.BestSpeed …
// gzip.BestCompression); invalid values are ignored.
func WithLevel(level int) Option {
	return func(c *Compressor) {
		switch level {
		case gzip.BestSpeed, gzip.BestCompression, gzip.DefaultCompression, gzip.HuffmanOnly, gzip.NoCompression:
			c.level = level
		}
	}
}

// New creates a Compressor with sensible defaults.
func New(opts ...Option) *Compressor {
	c := &Compressor{minSize: 1024, level: gzip.DefaultCompression}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Handler is the middleware entry point. Register with
// r.Use(compress.New().Handler).
func (c *Compressor) Handler(x *vigo.X) {
	if !c.eligible(x) {
		x.Next()
		return
	}
	orig := x.ResponseWriter()
	gw := newGzipResponseWriter(orig, c.minSize, c.level)
	x.SetResponseWriter(gw)
	defer x.SetResponseWriter(orig)
	defer gw.Close()
	x.Next()
}

// eligible decides on the request side whether compression may apply at all.
func (c *Compressor) eligible(x *vigo.X) bool {
	r := x.Request
	if r == nil {
		return false
	}
	// Bodyless requests, byte ranges and protocol upgrades are never touched.
	if r.Method == http.MethodHead {
		return false
	}
	if r.Header.Get("Range") != "" {
		return false
	}
	if r.Header.Get("Upgrade") != "" || strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return false
	}
	return acceptsGzip(r)
}

// acceptsGzip parses Accept-Encoding and reports whether gzip is acceptable.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					q = f
				}
			}
		}
		return q > 0
	}
	return false
}

// compressibleType reports whether a Content-Type benefits from gzip.
func compressibleType(ct string) bool {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		// No type set: the body is most likely text/JSON — worth compressing.
		return true
	}
	if strings.HasPrefix(ct, "text/") {
		return !strings.HasPrefix(ct, "text/event-stream")
	}
	switch ct {
	case "application/json", "application/javascript", "application/x-javascript",
		"application/xml", "application/xhtml+xml", "application/rss+xml", "application/atom+xml",
		"application/ld+json", "application/manifest+json", "application/vnd.api+json",
		"application/x-www-form-urlencoded", "image/svg+xml":
		return true
	}
	return false
}

// addVary appends a Vary value if not already present.
func addVary(h http.Header, value string) {
	for _, cur := range h.Values("Vary") {
		for _, tok := range strings.Split(cur, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), value) {
				return
			}
		}
	}
	h.Add("Vary", value)
}
