package vigo

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func getFreePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestConfigIsValidAppliesDefaults(t *testing.T) {
	cfg := &Config{
		Host: "localhost",
		Port: 8080,
	}

	if err := cfg.IsValid(); err != nil {
		t.Fatalf("expected localhost to be valid: %v", err)
	}
	if cfg.ReadTimeout <= 0 || cfg.ReadHeaderTimeout <= 0 || cfg.WriteTimeout <= 0 || cfg.IdleTimeout <= 0 {
		t.Fatalf("expected timeout defaults to be applied: %+v", cfg)
	}
	if cfg.ShutdownTimeout <= 0 {
		t.Fatalf("expected shutdown timeout default to be applied: %+v", cfg)
	}
	if cfg.MaxHeaderBytes <= 0 || cfg.PostMaxMemory == 0 {
		t.Fatalf("expected size limits to be applied: %+v", cfg)
	}
}

func TestApplicationShutdown(t *testing.T) {
	port := getFreePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	app, err := NewServer(
		WithHost("127.0.0.1"),
		WithPort(port),
		WithShutdownTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	r := NewRouter()
	r.Get("/health", func(x *X) error {
		return x.String(http.StatusOK, "ok")
	})
	app.SetRouter(r)

	runDone := make(chan error, 1)
	go func() {
		runDone <- app.Run()
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = client.Get(baseURL + "/health")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server did not become ready: %v", err)
	}

	resp, err = client.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("expected body ok, got %q", string(body))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run returned error after shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after shutdown")
	}
}

func TestRequestIDGeneratedAndReturned(t *testing.T) {
	app, err := NewServer(WithHost("127.0.0.1"), WithPort(8080))
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	r := NewRouter()
	r.Get("/id", func(x *X) error {
		if x.RequestID() == "" {
			t.Fatal("expected request id in context")
		}
		return x.String(http.StatusOK, "%s", x.RequestID())
	})
	app.SetRouter(r)

	req := httptest.NewRequest(http.MethodGet, "/id", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	headerID := w.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("expected generated request id header")
	}
	if body := w.Body.String(); body != headerID {
		t.Fatalf("expected body to match request id header, got body=%q header=%q", body, headerID)
	}
}

func TestRequestIDPreservesIncomingHeader(t *testing.T) {
	app, err := NewServer(WithHost("127.0.0.1"), WithPort(8080), WithRequestIDHeader("X-Trace-ID"))
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	r := NewRouter()
	r.Get("/id", func(x *X) error {
		return x.String(http.StatusOK, "%s", x.RequestID())
	})
	app.SetRouter(r)

	req := httptest.NewRequest(http.MethodGet, "/id", nil)
	req.Header.Set("X-Trace-ID", "trace-123")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if got := w.Header().Get("X-Trace-ID"); got != "trace-123" {
		t.Fatalf("expected response header to preserve incoming request id, got %q", got)
	}
	if got := w.Body.String(); got != "trace-123" {
		t.Fatalf("expected handler to observe incoming request id, got %q", got)
	}
}

func TestResponseCapturePreservesFlusher(t *testing.T) {
	app, err := NewServer(WithHost("127.0.0.1"), WithPort(8080))
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	r := NewRouter()
	r.Get("/stream", func(x *X) error {
		if _, err := x.WriteString("chunk"); err != nil {
			return err
		}
		x.Flush()
		return nil
	})
	app.SetRouter(r)

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if body := w.Body.String(); body != "chunk" {
		t.Fatalf("expected flushed body, got %q", body)
	}
}
