package plugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/contrib/common"
)

// TestHelper provides helper functions for testing plugins.
type TestHelper struct {
	t      *testing.T
	Router vigo.Router
	Loader *Loader
}

// NewTestHelper creates a new TestHelper with a default router and loader.
// It also sets up LocalDeps to use the local vigo source for dependencies.
func NewTestHelper(t *testing.T) *TestHelper {
	loader := NewLoader()

	wd, err := os.Getwd()
	if err == nil {
		rootDir := findProjectRoot(wd)
		if rootDir != "" {
			loader.LocalDeps["github.com/veypi/vigo"] = rootDir
		} else {
			// If not found, maybe log warning or fail?
			// For tests, we expect it to be found.
			t.Log("Warning: could not find project root for replacement")
		}
	} else {
		t.Logf("Warning: failed to get wd: %v", err)
	}

	router := vigo.NewRouter()
	// Add standard JSON response middleware to mimic main application behavior
	// This ensures that handlers returning structs or errors are correctly serialized
	router.After(common.JsonResponse, common.JsonErrorResponse)

	return &TestHelper{
		t:      t,
		Router: router,
		Loader: loader,
	}
}

func findProjectRoot(startDir string) string {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// Check if it's our module
			content, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
			if len(content) > 0 { // Simple check, ideally parse it
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// LoadSource loads a plugin from a source string.
func (h *TestHelper) LoadSource(prefix string, source string) {
	err := h.Loader.LoadSource(h.Router, prefix, []byte(source))
	if err != nil {
		h.t.Fatalf("Failed to load plugin source: %v", err)
	}
}

// Request performs a request against the router and returns the response recorder.
func (h *TestHelper) Request(method, path string, body interface{}) *httptest.ResponseRecorder {
	req, err := http.NewRequest(method, path, nil) // Body handling simplified for now
	if err != nil {
		h.t.Fatalf("Failed to create request: %v", err)
	}

	w := httptest.NewRecorder()
	h.Router.ServeHTTP(w, req)
	return w
}
