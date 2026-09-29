//
// compress_test.go
// Copyright (C) 2026 veypi <i@veypi.com>
// Distributed under terms of the MIT license.
//

package compress

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veypi/vigo"
)

func gunzipAll(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gzip stream: %v", err)
	}
	return out
}

func newTestRouter(t *testing.T, handler any, opts ...Option) vigo.Router {
	t.Helper()
	r := vigo.NewRouter()
	r.Use(New(opts...).Handler)
	r.Get("/t", handler)
	return r
}

func doGet(t *testing.T, r vigo.Router, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCompress_Gzip(t *testing.T) {
	content := strings.Repeat("hello world ", 500)
	r := newTestRouter(t, func(x *vigo.X) { _ = x.String(200, "%s", content) })
	rec := doGet(t, r, "/t", map[string]string{"Accept-Encoding": "gzip, deflate, br"})

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if v := strings.Join(rec.Header().Values("Vary"), ","); !strings.Contains(v, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want to contain Accept-Encoding", v)
	}
	if cl := rec.Header().Get("Content-Length"); cl != "" {
		t.Fatalf("Content-Length should be dropped, got %q", cl)
	}
	if body := gunzipAll(t, rec.Body.Bytes()); string(body) != content {
		t.Fatalf("body mismatch: got %d bytes, want %d", len(body), len(content))
	}
}

func TestCompress_NoAcceptEncoding(t *testing.T) {
	content := strings.Repeat("hello world ", 500)
	r := newTestRouter(t, func(x *vigo.X) { _ = x.String(200, "%s", content) })
	rec := doGet(t, r, "/t", nil)

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("unexpected Content-Encoding without Accept-Encoding")
	}
	if rec.Body.String() != content {
		t.Fatal("body mismatch")
	}
}

func TestCompress_PassthroughBinaryType(t *testing.T) {
	payload := bytes.Repeat([]byte{0x89, 0x50, 0x4e, 0x47}, 1000)
	r := newTestRouter(t, func(x *vigo.X) {
		x.Header().Set("Content-Type", "image/png")
		_, _ = x.Write(payload)
	})
	rec := doGet(t, r, "/t", map[string]string{"Accept-Encoding": "gzip"})

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("binary content should not be compressed")
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatal("payload mismatch")
	}
}

func TestCompress_PassthroughSmallKnownLength(t *testing.T) {
	r := newTestRouter(t, func(x *vigo.X) {
		x.Header().Set("Content-Type", "text/plain")
		x.Header().Set("Content-Length", "11")
		_, _ = x.Write([]byte("hello world"))
	})
	rec := doGet(t, r, "/t", map[string]string{"Accept-Encoding": "gzip"})

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("small body should not be compressed")
	}
}

func TestCompress_PassthroughRange(t *testing.T) {
	r := newTestRouter(t, func(x *vigo.X) { _ = x.String(200, "%s", strings.Repeat("x", 4096)) })
	rec := doGet(t, r, "/t", map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-99"})

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("range request should not be compressed")
	}
}

func TestCompress_PassthroughUpgrade(t *testing.T) {
	r := newTestRouter(t, func(x *vigo.X) { _ = x.String(200, "upgrade-ish") })
	rec := doGet(t, r, "/t", map[string]string{
		"Accept-Encoding": "gzip",
		"Upgrade":         "websocket",
		"Connection":      "Upgrade",
	})

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("upgrade request should not be compressed")
	}
}

func TestCompress_PassthroughSSE(t *testing.T) {
	payload := "data: one\n\ndata: two\n\n"
	r := newTestRouter(t, func(x *vigo.X) {
		x.Header().Set("Content-Type", "text/event-stream")
		_, _ = x.Write([]byte(payload))
	})
	rec := doGet(t, r, "/t", map[string]string{"Accept-Encoding": "gzip"})

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("SSE should not be compressed")
	}
	if rec.Body.String() != payload {
		t.Fatal("payload mismatch")
	}
}

func TestCompress_Passthrough304(t *testing.T) {
	r := newTestRouter(t, func(x *vigo.X) { x.WriteHeader(http.StatusNotModified) })
	rec := doGet(t, r, "/t", map[string]string{"Accept-Encoding": "gzip"})

	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("304 should not be compressed")
	}
	if rec.Code != http.StatusNotModified {
		t.Fatalf("code = %d, want 304", rec.Code)
	}
}

func TestCompress_Flush(t *testing.T) {
	r := newTestRouter(t, func(x *vigo.X) {
		_, _ = x.Write([]byte("part-one "))
		x.Flush()
		_, _ = x.Write([]byte("part-two"))
	})
	rec := doGet(t, r, "/t", map[string]string{"Accept-Encoding": "gzip"})

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("expected gzip")
	}
	if body := gunzipAll(t, rec.Body.Bytes()); string(body) != "part-one part-two" {
		t.Fatalf("body = %q", body)
	}
}

func TestCompress_File(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("0123456789abcdef", 512)), 0o644); err != nil {
		t.Fatal(err)
	}
	small := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(small, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := vigo.NewRouter()
	r.Use(New().Handler)
	r.Get("/big", func(x *vigo.X) { _ = x.File(big) })
	r.Get("/small", func(x *vigo.X) { _ = x.File(small) })

	rec := doGet(t, r, "/big", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("big file should be compressed (CE=%q, CL=%q)",
			rec.Header().Get("Content-Encoding"), rec.Header().Get("Content-Length"))
	}
	want, err := os.ReadFile(big)
	if err != nil {
		t.Fatal(err)
	}
	if got := gunzipAll(t, rec.Body.Bytes()); !bytes.Equal(got, want) {
		t.Fatal("big file body mismatch")
	}

	rec = doGet(t, r, "/small", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("small file should not be compressed")
	}
	if rec.Body.String() != "tiny" {
		t.Fatalf("small body = %q", rec.Body.String())
	}
}

func TestCompress_RealServer(t *testing.T) {
	content := strings.Repeat("hello world ", 500)
	r := newTestRouter(t, func(x *vigo.X) { _ = x.String(200, "%s", content) })
	srv := httptest.NewServer(r)
	defer srv.Close()

	// 1) default client: transparent gzip negotiation + decompression
	resp, err := http.Get(srv.URL + "/t")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != content {
		t.Fatalf("transparent client body mismatch: got %d bytes, want %d", len(body), len(content))
	}

	// 2) raw client: explicit Accept-Encoding, manual decompression
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/t", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body2, err := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", resp2.Header.Get("Content-Encoding"))
	}
	if got := gunzipAll(t, body2); string(got) != content {
		t.Fatalf("raw client body mismatch after gunzip: got %d bytes, want %d", len(got), len(content))
	}
}

type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestCompress_HijackPassThrough(t *testing.T) {
	hr := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	gw := newGzipResponseWriter(hr, 1024, gzip.DefaultCompression)
	if _, _, err := gw.Hijack(); err != nil {
		t.Fatalf("Hijack: %v", err)
	}
	if !hr.hijacked {
		t.Fatal("underlying Hijack was not called")
	}
	if !gw.passed {
		t.Fatal("hijacked writer should end up in passthrough mode")
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		ae   string
		want bool
	}{
		{"", false},
		{"gzip", true},
		{" GZIP ", true},
		{"deflate, gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"br, zstd", false},
		{"deflate", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.ae != "" {
			req.Header.Set("Accept-Encoding", c.ae)
		}
		if got := acceptsGzip(req); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", c.ae, got, c.want)
		}
	}
}

func TestCompressibleType(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"text/html; charset=utf-8", true},
		{"application/json", true},
		{"application/javascript", true},
		{"image/svg+xml", true},
		{"text/event-stream", false},
		{"image/png", false},
		{"application/octet-stream", false},
		{"video/mp4", false},
		{"", true},
	}
	for _, c := range cases {
		if got := compressibleType(c.ct); got != c.want {
			t.Errorf("compressibleType(%q) = %v, want %v", c.ct, got, c.want)
		}
	}
}
