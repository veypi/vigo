//
// writer.go
// Copyright (C) 2026 veypi <i@veypi.com>
// Distributed under terms of the MIT license.
//

package compress

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"net"
	"net/http"
	"strconv"
)

// gzipResponseWriter wraps the underlying ResponseWriter and decides lazily,
// at the first WriteHeader/Write/Flush call, whether the response is
// compressed or passed through untouched. The decision is based on headers
// already set by the handler (Content-Type / Content-Encoding /
// Content-Length) and the status code.
type gzipResponseWriter struct {
	http.ResponseWriter
	minSize int
	level   int

	gz     *gzip.Writer
	active bool // decision made: compressing
	passed bool // decision made: pass through untouched
}

func newGzipResponseWriter(w http.ResponseWriter, minSize, level int) *gzipResponseWriter {
	return &gzipResponseWriter{ResponseWriter: w, minSize: minSize, level: level}
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	w.decide(code)
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if !w.active && !w.passed {
		w.decide(http.StatusOK)
	}
	if w.passed || w.gz == nil {
		return w.ResponseWriter.Write(p)
	}
	return w.gz.Write(p)
}

// Flush implements http.Flusher.
func (w *gzipResponseWriter) Flush() {
	if !w.active && !w.passed {
		w.decide(http.StatusOK)
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker so protocol upgrades (e.g. WebSocket) keep
// working even when a request slipped past the middleware-level upgrade check.
func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.active && !w.passed {
		w.passed = true
	}
	if w.active {
		return nil, nil, fmt.Errorf("compress: response is already being compressed")
	}
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("compress: underlying ResponseWriter does not support Hijack")
	}
	return hj.Hijack()
}

// Unwrap exposes the underlying writer (http.ResponseController support).
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Close finishes the gzip stream. It is a no-op for passthrough responses.
func (w *gzipResponseWriter) Close() {
	if w.gz != nil {
		_ = w.gz.Close()
	}
}

// decide evaluates the response once and either starts gzip or falls back to
// passthrough.
func (w *gzipResponseWriter) decide(code int) {
	if w.active || w.passed {
		return
	}
	h := w.Header()

	// 1xx informational; 204/205/304 have no body; 206 ranges must stay
	// byte-exact.
	if code < 200 || code == http.StatusNoContent || code == http.StatusResetContent ||
		code == http.StatusNotModified || code == http.StatusPartialContent {
		w.passed = true
		return
	}
	// Do not double-encode.
	if h.Get("Content-Encoding") != "" {
		w.passed = true
		return
	}
	// Known small bodies are not worth gzip.
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n < int64(w.minSize) {
			w.passed = true
			return
		}
	}
	if !compressibleType(h.Get("Content-Type")) {
		w.passed = true
		return
	}

	gz, err := gzip.NewWriterLevel(w.ResponseWriter, w.level)
	if err != nil {
		w.passed = true
		return
	}
	w.gz = gz
	w.active = true
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	addVary(h, "Accept-Encoding")
}
