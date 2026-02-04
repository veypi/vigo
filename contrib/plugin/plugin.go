package plugin

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	goplugin "plugin"
	"strings"

	"github.com/veypi/vigo"
)

// Loader handles dynamic plugin loading with configurable security policies.
type Loader struct {
	// AllowedPrefixes is a list of package prefixes that are allowed to be imported.
	// e.g. "fmt", "github.com/veypi/vigo"
	AllowedPrefixes []string

	// ForbiddenPrefixes is a list of package prefixes that are forbidden to be imported.
	// This takes precedence over AllowedPrefixes.
	// e.g. "github.com/veypi/vigo/contrib"
	ForbiddenPrefixes []string

	// ForbiddenSelectors forbids specific function calls on packages.
	// Key is the package import path, Value is list of forbidden function names.
	// e.g. "gorm.io/gorm": []string{"Open", "OpenDB"}
	ForbiddenSelectors map[string][]string

	// AllowImportAlias controls whether import aliasing is allowed.
	// If false, imports like `import m "math"` will be rejected.
	AllowImportAlias bool

	// CompileDir is the directory where plugins are compiled to.
	// Defaults to "~/.vigo/plugin/".
	CompileDir string

	// LocalDeps specifies local dependencies for replacement in go.mod.
	// Key is the module path, Value is the local file path.
	// e.g. "github.com/veypi/vigo": "/path/to/local/vigo"
	LocalDeps map[string]string
}

// NewLoader creates a Loader with default strict security settings.
func NewLoader() *Loader {
	return &Loader{
		AllowedPrefixes: DefaultAllowedPrefixes(),
		ForbiddenPrefixes: []string{
			"github.com/veypi/vigo/contrib",
		},
		ForbiddenSelectors: map[string][]string{
			"gorm.io/gorm":            {"Open", "OpenDB"},
			"github.com/veypi/vigo": {"New"},
		},
		AllowImportAlias: false,
		CompileDir:       filepath.Join(os.TempDir(), "vigo"),
		LocalDeps:        make(map[string]string),
	}
}

// DefaultAllowedPrefixes returns the default whitelist.
func DefaultAllowedPrefixes() []string {
	return []string{
		"github.com/veypi/vigo",
		"gorm.io/gorm",
		"strings",
		"bytes",
		"fmt",
		"time",
		"encoding",
		"errors",
		"context",
		"io",
		"sort",
		"strconv",
		"regexp",
		"path",
		"unicode",
		"sync",
		"reflect",
		"log",
		"math",
		"mime",
	}
}

// Load loads a plugin from a file path and mounts its "Router" to the parent router at prefix.
// The plugin file must end in .go.
func (l *Loader) Load(r vigo.Router, prefix string, p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() || !strings.HasSuffix(p, ".go") {
		return fmt.Errorf("invalid plugin path: %s", p)
	}

	content, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	return l.loadContent(r, prefix, content, filepath.Base(p))
}

// LoadSource loads a plugin from source content and mounts its "Router" to the parent router at prefix.
func (l *Loader) LoadSource(r vigo.Router, prefix string, content []byte) error {
	return l.loadContent(r, prefix, content, "main.go")
}

func (l *Loader) loadContent(r vigo.Router, prefix string, content []byte, filename string) error {
	// Determine compile dir
	baseDir, err := expandPath(l.CompileDir)
	if err != nil {
		return fmt.Errorf("failed to expand compile dir: %w", err)
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create compile dir: %w", err)
	}

	// Create a unique temporary directory for this build
	buildDir, err := os.MkdirTemp(baseDir, "build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp build dir: %w", err)
	}
	// defer os.RemoveAll(buildDir)

	// Write source file
	srcPath := filepath.Join(buildDir, filename)
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write source content: %w", err)
	}

	// Security checks
	if err := l.checkDependencies(srcPath); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}
	if err := l.checkCodeSafety(srcPath); err != nil {
		return fmt.Errorf("safety check failed: %w", err)
	}

	// Compile
	soPath, err := l.compile(buildDir, srcPath)
	if err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	// Open plugin
	p, err := goplugin.Open(soPath)
	if err != nil {
		return fmt.Errorf("failed to open plugin: %w", err)
	}

	// Optional: Call Init if exists
	initSym, err := p.Lookup("Init")
	if err == nil {
		if initFunc, ok := initSym.(func() error); ok {
			if err := initFunc(); err != nil {
				return fmt.Errorf("plugin init failed: %w", err)
			}
		}
	}

	// Lookup Router
	sym, err := p.Lookup("Router")
	if err != nil {
		return fmt.Errorf("plugin does not export 'Router'")
	}

	// Verify type
	routerPtr, ok := sym.(*vigo.Router)
	if !ok {
		return fmt.Errorf("plugin symbol 'Router' is not of type *vigo.Router, got %T", sym)
	}

	if *routerPtr == nil {
		return fmt.Errorf("plugin exported 'Router' is nil")
	}

	// Extend
	r.Extend(prefix, *routerPtr)
	return nil
}

func (l *Loader) compile(buildDir, srcPath string) (string, error) {
	soPath := filepath.Join(buildDir, "plugin.so")
	absSrcPath, err := filepath.Abs(srcPath)
	if err != nil {
		return "", err
	}

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", soPath, absSrcPath)
	cmd.Env = os.Environ()
	cmd.Dir = buildDir

	// Setup go.mod
	if err := l.autoGenerateGoMod(buildDir); err != nil {
		// Just log warning? Or fail?
		// For plugins depending on vigo, we almost certainly need it.
		// But maybe user provided a go.mod in content?
		// We only wrote one file.
		// So we must generate it.
		return "", err
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build error: %s, output: %s", err, output)
	}

	return soPath, nil
}

// Load is a convenience function using the default Loader.
func Load(r vigo.Router, prefix string, path string) error {
	return NewLoader().Load(r, prefix, path)
}

func (l *Loader) checkDependencies(path string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	for _, imp := range node.Imports {
		// Check for aliases
		if !l.AllowImportAlias {
			if imp.Name != nil && imp.Name.Name != "." && imp.Name.Name != "_" {
				return fmt.Errorf("import aliases are forbidden: %s as %s", imp.Path.Value, imp.Name.Name)
			}
			// Strict mode: also forbid dot imports if not explicitly handled?
			// Usually dot imports are discouraged anyway.
			if imp.Name != nil && imp.Name.Name == "." {
				return fmt.Errorf("dot imports are forbidden: %s", imp.Path.Value)
			}
		}

		// Remove quotes
		pkgPath := strings.Trim(imp.Path.Value, "\"")

		if !l.isAllowedPackage(pkgPath) {
			return fmt.Errorf("forbidden dependency: %s", pkgPath)
		}
	}
	return nil
}

func (l *Loader) isAllowedPackage(pkg string) bool {
	// Check forbidden first
	for _, prefix := range l.ForbiddenPrefixes {
		if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
			return false
		}
	}

	// Check allowed
	for _, prefix := range l.AllowedPrefixes {
		if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
			return true
		}
	}
	return false
}

func (l *Loader) checkCodeSafety(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	// Map local package names to import paths
	// localName -> importPath
	imports := make(map[string]string)

	for _, imp := range node.Imports {
		pkgPath := strings.Trim(imp.Path.Value, "\"")
		var localName string
		if imp.Name != nil {
			localName = imp.Name.Name
		} else {
			// Best effort to guess package name from path
			// For standard lib and common convention, it's the last element
			localName = path.Base(pkgPath)
		}
		imports[localName] = pkgPath
	}

	// Scan for forbidden function calls
	var safetyErr error
	ast.Inspect(node, func(n ast.Node) bool {
		if safetyErr != nil {
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		// Check for X.Sel
		if xIdent, ok := sel.X.(*ast.Ident); ok {
			// Check if X is a package name
			if importPath, isPkg := imports[xIdent.Name]; isPkg {
				// Check if this package has forbidden selectors
				if forbidden, hasForbidden := l.ForbiddenSelectors[importPath]; hasForbidden {
					for _, fn := range forbidden {
						if sel.Sel.Name == fn {
							safetyErr = fmt.Errorf("calling %s.%s is forbidden", xIdent.Name, sel.Sel.Name)
							return false
						}
					}
				}
			}
		}

		return true
	})

	return safetyErr
}

func (l *Loader) autoGenerateGoMod(dir string) error {
	// 1. go mod init
	cmd := exec.Command("go", "mod", "init", "plugin_build")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod init failed: %s", out)
	}

	// 2. Add replacements from LocalDeps
	for module, path := range l.LocalDeps {
		replaceCmd := fmt.Sprintf("%s=%s", module, path)
		cmd := exec.Command("go", "mod", "edit", "-replace", replaceCmd)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("replace %s failed: %s", module, out)
		}
	}

	// 3. go mod tidy
	// This will resolve dependencies based on imports in the source file
	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		// Fallback: explicit get if tidy fails (e.g. network issues without replace)
		// But if we have replace, tidy should work.
		return fmt.Errorf("go mod tidy failed: %s", out)
	}

	return nil
}

func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
