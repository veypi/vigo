package main

import (
	"strings"
	"testing"

	"github.com/veypi/vigo/contrib/plugin"
)

const aliasPluginSource = `
package main

import (
	f "fmt"
	"github.com/veypi/vigo"
)

var Router = vigo.NewRouter()

func init() {
	Router.Get("/alias", func(x *vigo.X) error {
		return x.JSON(f.Sprintf("Alias check"))
	})
}
`

func TestMyPluginAlias(t *testing.T) {
	helper := plugin.NewTestHelper(t)

	// 1. Default: Should fail because aliases are not allowed
	if err := helper.Loader.LoadSource(helper.Router, "/alias_fail", []byte(aliasPluginSource)); err == nil {
		t.Error("Expected error for import alias, got nil")
	}

	// 2. Enable AllowImportAlias: Should succeed
	helper.Loader.AllowImportAlias = true
	if err := helper.Loader.LoadSource(helper.Router, "/alias_ok", []byte(aliasPluginSource)); err != nil {
		t.Errorf("Expected success with AllowImportAlias=true, got error: %v", err)
	} else {
		// Verify route
		w := helper.Request("GET", "/alias_ok/alias", nil)
		if w.Code != 200 {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Alias check") {
			t.Errorf("Expected response containing 'Alias check', got '%s'", w.Body.String())
		}
	}
}
