package common

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/veypi/vigo"
)

func TestJsonErrorResponseSetsJSONContentType(t *testing.T) {
	r := vigo.NewRouter()
	r.After(JsonResponse, JsonErrorResponse)
	r.Get("/err", func(x *vigo.X) error {
		return errors.New("boom")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON content type, got %q", got)
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestJsonResponseDoesNotOverrideStatusCode(t *testing.T) {
	r := vigo.NewRouter()
	r.After(JsonResponse, JsonErrorResponse)
	r.Get("/created", func(x *vigo.X) (any, error) {
		x.WriteHeader(http.StatusCreated)
		return map[string]string{"ok": "true"}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/created", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected existing status code to be preserved, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON content type, got %q", got)
	}
}
