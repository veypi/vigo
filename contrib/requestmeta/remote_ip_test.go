package requestmeta

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/veypi/vigo"
)

func TestRemoteIPTrustsConfiguredProxy(t *testing.T) {
	app, err := vigo.NewServer(vigo.WithHost("127.0.0.1"), vigo.WithPort(8080), vigo.WithTrustedProxies("127.0.0.1", "10.0.0.0/8"))
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	r := vigo.NewRouter()
	r.Get("/ip", func(x *vigo.X) error { return x.String(http.StatusOK, "%s", RemoteIP(x)) })
	app.SetRouter(r)

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 127.0.0.1")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if got := w.Body.String(); got != "203.0.113.10" {
		t.Fatalf("expected forwarded IP, got %q", got)
	}
}

func TestRemoteIPIgnoresForwardedHeadersWithoutTrustedProxy(t *testing.T) {
	app, err := vigo.NewServer(vigo.WithHost("127.0.0.1"), vigo.WithPort(8080))
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	r := vigo.NewRouter()
	r.Get("/ip", func(x *vigo.X) error { return x.String(http.StatusOK, "%s", RemoteIP(x)) })
	app.SetRouter(r)

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("X-Real-IP", "198.51.100.7")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if got := w.Body.String(); got != "127.0.0.1" {
		t.Fatalf("expected direct remote IP, got %q", got)
	}
}
