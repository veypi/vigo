package main

import (
	"strings"
	"testing"

	"github.com/veypi/vigo/contrib/plugin"
)

const forbiddenPrefixPluginSource = `
package main

import (
	"fmt"
	"github.com/veypi/vigo"
	// This should be forbidden by default because it's in ForbiddenPrefixes
	"github.com/veypi/vigo/contrib/plugin" 
)

var Router = vigo.NewRouter()

func init() {
	Router.Get("/forbidden_prefix", func(x *vigo.X) error {
		// Use something from the forbidden package
		_ = plugin.DefaultAllowedPrefixes()
		return x.JSON(fmt.Sprintf("Forbidden prefix check"))
	})
}
`

func TestMyPluginForbiddenPrefix(t *testing.T) {
	helper := plugin.NewTestHelper(t)

	// Should fail because vigo/contrib is forbidden
	err := helper.Loader.LoadSource(helper.Router, "/forbidden_prefix", []byte(forbiddenPrefixPluginSource))
	if err == nil {
		t.Error("Expected error for forbidden prefix, got nil")
	} else {
		if !strings.Contains(err.Error(), "forbidden dependency: github.com/veypi/vigo/contrib") {
			t.Errorf("Expected forbidden prefix error, got: %v", err)
		}
	}

	// 2. Clear ForbiddenPrefixes: Should succeed
	// We need to create a new loader or modify the existing one.
	// Modifying existing one is fine.
	helper.Loader.ForbiddenPrefixes = []string{}

	// Reload
	if err := helper.Loader.LoadSource(helper.Router, "/forbidden_prefix_ok", []byte(forbiddenPrefixPluginSource)); err != nil {
		t.Errorf("Expected success after clearing ForbiddenPrefixes, got error: %v", err)
	} else {
		w := helper.Request("GET", "/forbidden_prefix_ok/forbidden_prefix", nil)
		if w.Code != 200 {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Forbidden prefix check") {
			t.Errorf("Expected response containing 'Forbidden prefix check', got '%s'", w.Body.String())
		}
	}
}
