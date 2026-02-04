package main

import (
	"strings"
	"testing"

	"github.com/veypi/vigo/contrib/plugin"
)

const badPluginSource = `
package main

import (
	"fmt"
	"github.com/veypi/vigo"
	"github.com/gin-gonic/gin" // Forbidden dependency
)

var Router = vigo.NewRouter()

func init() {
	Router.Get("/bad", func(x *vigo.X) any {
		return fmt.Sprintf("Bad: %v", gin.Mode())
	})
}
`

func TestMyPluginBad(t *testing.T) {
	helper := plugin.NewTestHelper(t)

	// We skip go mod tidy because we expect dependency check to fail before compilation.
	// Also, go mod tidy would fail because we didn't add gin to go.mod.
	// The Loader checks dependencies by parsing the AST, so it should catch it.

	// Should fail because gin is not allowed
	err := helper.Loader.LoadSource(helper.Router, "/bad", []byte(badPluginSource))
	if err == nil {
		t.Error("Expected error for forbidden dependency, got nil")
	} else {
		if !strings.Contains(err.Error(), "forbidden dependency: github.com/gin-gonic/gin") {
			t.Errorf("Expected forbidden dependency error, got: %v", err)
		}
	}
}
