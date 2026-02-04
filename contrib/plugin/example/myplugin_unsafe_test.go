package main

import (
	"strings"
	"testing"

	"github.com/veypi/vigo/contrib/plugin"
)

const unsafePluginSource = `
package main

import (
	"fmt"
	"github.com/veypi/vigo"
)

var Router = vigo.NewRouter()

func init() {
	Router.Get("/unsafe", func(x *vigo.X) error {
		// Let's try to call vigo.New() which is forbidden by default loader
		_, _ = vigo.New()
		
		return x.JSON(fmt.Sprintf("Unsafe check"))
	})
}
`

func TestMyPluginUnsafe(t *testing.T) {
	helper := plugin.NewTestHelper(t)
	// Should fail because vigo.New is forbidden
	err := helper.Loader.LoadSource(helper.Router, "/unsafe", []byte(unsafePluginSource))
	if err == nil {
		t.Error("Expected error for unsafe function call, got nil")
	} else {
		if !strings.Contains(err.Error(), "calling vigo.New is forbidden") {
			t.Errorf("Expected forbidden selector error, got: %v", err)
		}
	}

	// 2. Clear ForbiddenSelectors: Should succeed
	delete(helper.Loader.ForbiddenSelectors, "github.com/veypi/vigo")

	// Reload
	if err := helper.Loader.LoadSource(helper.Router, "/unsafe_ok", []byte(unsafePluginSource)); err != nil {
		t.Errorf("Expected success after clearing ForbiddenSelectors, got error: %v", err)
	} else {
		w := helper.Request("GET", "/unsafe_ok/unsafe", nil)
		if w.Code != 200 {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Unsafe check") {
			t.Errorf("Expected response containing 'Unsafe check', got '%s'", w.Body.String())
		}
	}
}
