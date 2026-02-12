package vigo

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPipeline_ExecutionOrder verifies the Onion Model execution order:
// Parent Before -> Child Before -> Handler -> Child After -> Parent After
func TestPipeline_ExecutionOrder(t *testing.T) {
	r := NewRouter()
	steps := []string{}

	// Parent Router
	r.Use(func(x *X) {
		steps = append(steps, "Parent Before Start")
		x.Next()
		steps = append(steps, "Parent Before End")
	})
	r.After(func(x *X) {
		steps = append(steps, "Parent After")
	})

	// Child Router
	sub := r.SubRouter("/sub")
	sub.Use(func(x *X) {
		steps = append(steps, "Child Before Start")
		x.Next()
		steps = append(steps, "Child Before End")
	})
	sub.After(func(x *X) {
		steps = append(steps, "Child After")
	})

	// Handler
	sub.Get("/test", func(x *X) {
		steps = append(steps, "Handler")
	})

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/sub/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify
	expected := []string{
		"Parent Before Start",
		"Child Before Start",
		"Handler",
		"Child After",
		"Parent After",
		"Child Before End",
		"Parent Before End",
	}

	if len(steps) != len(expected) {
		t.Fatalf("Expected %d steps, got %d: %v", len(expected), len(steps), steps)
	}

	for i, step := range steps {
		if step != expected[i] {
			t.Errorf("Step %d: expected %q, got %q", i, expected[i], step)
		}
	}
}

// TestPipeline_Stop verifies that x.Stop() prevents subsequent handlers from executing
func TestPipeline_Stop(t *testing.T) {
	r := NewRouter()
	executed := []string{}

	r.Use(func(x *X) {
		executed = append(executed, "Middleware 1")
		x.Stop()
	})

	r.Use(func(x *X) {
		executed = append(executed, "Middleware 2") // Should not execute
	})

	r.Get("/test", func(x *X) {
		executed = append(executed, "Handler") // Should not execute
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if len(executed) != 1 || executed[0] != "Middleware 1" {
		t.Errorf("Expected only 'Middleware 1', got %v", executed)
	}
}

// TestPipeline_ErrorHandling verifies that returning an error interrupts the flow
// and triggers error handlers if present.
func TestPipeline_ErrorHandling(t *testing.T) {
	r := NewRouter()
	steps := []string{}

	r.Use(func(x *X) error {
		steps = append(steps, "Middleware 1")
		return errors.New("pipeline error")
	})

	r.Use(func(x *X) {
		steps = append(steps, "Middleware 2") // Should not execute
	})

	r.Get("/test", func(x *X) {
		steps = append(steps, "Handler") // Should not execute
	})

	// Error Handler
	r.Use(func(x *X, err error) error {
		steps = append(steps, "ErrorHandler: "+err.Error())
		return nil // Error handled
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	expected := []string{
		"Middleware 1",
		"ErrorHandler: pipeline error",
	}

	if len(steps) != len(expected) {
		t.Fatalf("Expected %d steps, got %d: %v", len(expected), len(steps), steps)
	}

	for i, step := range steps {
		if step != expected[i] {
			t.Errorf("Step %d: expected %q, got %q", i, expected[i], step)
		}
	}
}

// TestPipeline_SkipBefore verifies that SkipBefore skips parent Before middlewares
func TestPipeline_SkipBefore(t *testing.T) {
	r := NewRouter()
	steps := []string{}

	r.Use(func(x *X) {
		steps = append(steps, "Parent Before")
	})
	r.After(func(x *X) {
		steps = append(steps, "Parent After")
	})

	// Normal route
	r.Get("/normal", func(x *X) {
		steps = append(steps, "Normal Handler")
	})

	// Skipped route
	r.Get("/skipped", SkipBefore, func(x *X) {
		steps = append(steps, "Skipped Handler")
	})

	// Test Normal
	req := httptest.NewRequest(http.MethodGet, "/normal", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Reset steps for Skipped test
	normalSteps := make([]string, len(steps))
	copy(normalSteps, steps)
	steps = []string{}

	// Test Skipped
	req = httptest.NewRequest(http.MethodGet, "/skipped", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify Normal
	expectedNormal := []string{"Parent Before", "Normal Handler", "Parent After"}
	if len(normalSteps) != len(expectedNormal) {
		t.Errorf("Normal: expected %v, got %v", expectedNormal, normalSteps)
	}

	// Verify Skipped (Parent Before should be missing, Parent After should remain)
	expectedSkipped := []string{"Skipped Handler", "Parent After"}
	if len(steps) != len(expectedSkipped) {
		t.Errorf("Skipped: expected %v, got %v", expectedSkipped, steps)
	}
}

// TestPipeline_PipeValue verifies data passing via PipeValue
func TestPipeline_PipeValue(t *testing.T) {
	r := NewRouter()

	r.Use(func(x *X) any {
		return "Hello"
	})

	r.Get("/test", func(x *X, val any) {
		str, ok := val.(string)
		if !ok {
			x.String(500, "Expected string")
			return
		}
		x.String(200, "%s World", str)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Body.String() != "Hello World" {
		t.Errorf("Expected 'Hello World', got %q", w.Body.String())
	}
}

// TestPipeline_Standardize verifies the type-safe handler wrapper
func TestPipeline_Standardize(t *testing.T) {
	r := NewRouter()

	type QueryRequest struct {
		Name string `src:"query"`
	}

	queryHandler := func(x *X, req *QueryRequest) (any, error) {
		return map[string]string{"msg": "Hello " + req.Name}, nil
	}

	jsonResponse := func(x *X, data any) error {
		return x.JSON(data)
	}

	r.Get("/query", queryHandler, jsonResponse)

	req := httptest.NewRequest(http.MethodGet, "/query?Name=Vigo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	// Output should be JSON {"msg":"Hello Vigo"}
	expected := `{"msg":"Hello Vigo"}`
	if w.Body.String() != expected {
		t.Errorf("Expected %q, got %q", expected, w.Body.String())
	}
}

// TestPipeline_AutoStandardize verifies automatic type-safe handler registration
func TestPipeline_AutoStandardize(t *testing.T) {
	r := NewRouter()

	type AutoRequest struct {
		ID string `src:"path"`
	}

	// Handler without explicit Standardize() call
	autoHandler := func(x *X, req *AutoRequest) (any, error) {
		return map[string]string{"id": req.ID}, nil
	}

	jsonResponse := func(x *X, data any) error {
		return x.JSON(data)
	}

	// Register directly
	r.Get("/auto/{ID}", autoHandler, jsonResponse)

	req := httptest.NewRequest(http.MethodGet, "/auto/123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	expected := `{"id":"123"}`
	if w.Body.String() != expected {
		t.Errorf("Expected %q, got %q", expected, w.Body.String())
	}
}
