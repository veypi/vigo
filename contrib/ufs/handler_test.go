package ufs

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/veypi/vigo"
)

func TestNewHandler(t *testing.T) {
	// 1. Prepare FS
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("Hello World"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "sub", "foo.txt"), []byte("Foo"), 0644)

	myFS := New(tmpDir)

	// 2. Prepare Router
	r := vigo.NewRouter()

	// Add simple error handler to reflect errors in status code
	r.After(func(x *vigo.X, err error) error {
		if err != nil {
			if e, ok := err.(*vigo.Error); ok {
				x.WriteHeader(e.Code)
			} else {
				x.WriteHeader(500)
			}
		}
		return nil
	})

	// Use {path:*} to capture path
	r.Get("/static/{path:*}", myFS.NewHandler())

	// 3. Test File
	req := httptest.NewRequest("GET", "/static/hello.txt", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("File: Expected 200, got %d", w.Code)
	}
	if w.Body.String() != "Hello World" {
		t.Errorf("File: Expected 'Hello World', got '%s'", w.Body.String())
	}

	// 4. Test Directory Listing
	req = httptest.NewRequest("GET", "/static/sub", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Dir: Expected 200, got %d", w.Code)
	}

	var entries []struct {
		Name string `json:"name"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Dir: Failed to parse JSON: %v, body: %s", err, w.Body.String())
	}
	if len(entries) != 1 || entries[0].Name != "foo.txt" {
		t.Errorf("Dir: Expected 1 entry 'foo.txt', got %v", entries)
	}

	// 5. Test 404
	req = httptest.NewRequest("GET", "/static/ghost.txt", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("404: Expected 404, got %d", w.Code)
	}
}

func TestNewHandlerWithDefaultFile(t *testing.T) {
	// 1. Prepare FS
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte("Index Page"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "style.css"), []byte("body {}"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "assets"), 0755)

	myFS := New(tmpDir)

	// 2. Prepare Router
	r := vigo.NewRouter()

	// Error handler
	r.After(func(x *vigo.X, err error) error {
		if err != nil {
			if e, ok := err.(*vigo.Error); ok {
				x.WriteHeader(e.Code)
			} else {
				x.WriteHeader(500)
			}
		}
		return nil
	})

	// Register with default file
	r.Get("/spa/{path:*}", myFS.NewHandlerWithDefaultFile("index.html"))

	// 3. Test Normal File
	req := httptest.NewRequest("GET", "/spa/style.css", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "body {}" {
		t.Errorf("Normal file failed: %d %s", w.Code, w.Body.String())
	}

	// 4. Test Directory -> Default
	req = httptest.NewRequest("GET", "/spa/assets", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "Index Page" {
		t.Errorf("Directory fallback failed: %d %s", w.Code, w.Body.String())
	}

	// 5. Test Not Found -> Default
	req = httptest.NewRequest("GET", "/spa/unknown", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "Index Page" {
		t.Errorf("404 fallback failed: %d %s", w.Code, w.Body.String())
	}

	// 6. Test Not Found with extension -> 404
	req = httptest.NewRequest("GET", "/spa/missing.js", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("404 with extension failed: %d", w.Code)
	}
}
