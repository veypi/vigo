package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veypi/vigo/contrib/plugin"
)

func TestMyPlugin(t *testing.T) {
	helper := plugin.NewTestHelper(t)

	// Load the plugin using the file path
	wd, _ := os.Getwd()
	pluginPath := filepath.Join(wd, "main.go")

	if err := helper.Loader.Load(helper.Router, "/plugin", pluginPath); err != nil {
		t.Fatalf("Failed to load plugin: %v", err)
	}

	// Test /hello
	w := helper.Request("GET", "/plugin/hello", nil)
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Hello from Plugin!") {
		t.Errorf("Expected 'Hello from Plugin!', got %s", w.Body.String())
	}

	// Test /echo
	w = helper.Request("GET", "/plugin/echo/world", nil)
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	// JSON response will be quoted string
	if !strings.Contains(w.Body.String(), "Echo: world") {
		t.Errorf("Expected 'Echo: world', got %s", w.Body.String())
	}

	// Test /user (Struct return)
	w = helper.Request("GET", "/plugin/user", nil)
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"name":"Vigo"`) {
		t.Errorf("Expected JSON with name:Vigo, got %s", w.Body.String())
	}
}
