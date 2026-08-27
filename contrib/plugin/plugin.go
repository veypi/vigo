package plugin

import (
	"encoding/json"
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
			"gorm.io/gorm":          {"Open", "OpenDB"},
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

	// 对齐插件模块依赖版本到主模块上下文：go.work workspace 会提升依赖版本
	// （如 google/uuid v1.3.0 → v1.6.0），而插件独立 tidy 只按主 go.mod 最低约束
	// 解析，版本不一致会导致 plugin.Open 报 "built with a different version"。
	if err := l.alignDependencyVersions(buildDir, l.mainContext(buildDir)); err != nil {
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

// mainContext 返回依赖版本对齐用的主模块上下文目录：
// 优先 buildDir 向上命中的 go.work 所在目录，其次 LocalDeps 中第一个存在的目录。
func (l *Loader) mainContext(buildDir string) string {
	if dir := findUpFile(buildDir, "go.work"); dir != "" {
		return dir
	}
	for _, p := range l.LocalDeps {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}

func findUpFile(startDir, name string) string {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// alignDependencyVersions 让插件模块 go.mod 的依赖版本与主模块上下文保持一致。
// go.work workspace 模式下 MVS 会提升依赖版本（如 google/uuid v1.3.0 → v1.6.0），
// 而插件在临时目录独立构建，tidy 只按主 go.mod 的最低约束解析出旧版本，
// 插件 .so 与主进程共享包版本不一致会导致 plugin.Open 报
// "plugin was built with a different version of package ..."。
// 显式 require 是硬约束，go mod tidy 不会降级，因此对齐后构建结果稳定。
func (l *Loader) alignDependencyVersions(buildDir, ctxDir string) error {
	if ctxDir == "" {
		return nil
	}

	cmd := exec.Command("go", "mod", "edit", "-json")
	cmd.Dir = buildDir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go mod edit -json failed: %s", out)
	}
	var mod struct {
		Require []struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"Require"`
		Replace []struct {
			Path string `json:"Path"`
		} `json:"Replace"`
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return fmt.Errorf("parse go.mod failed: %w", err)
	}

	replaced := make(map[string]bool, len(mod.Replace))
	for _, r := range mod.Replace {
		replaced[r.Path] = true
	}

	changed := false
	for _, req := range mod.Require {
		if replaced[req.Path] {
			continue // replace 到本地的模块（如 vigo）不参与远程版本对齐
		}
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}", req.Path)
		cmd.Dir = ctxDir
		ver, err := cmd.Output()
		if err != nil {
			continue // 插件独有依赖不在主依赖图，保持 tidy 结果
		}
		v := strings.TrimSpace(string(ver))
		if v == "" || v == req.Version {
			continue
		}
		cmd = exec.Command("go", "mod", "edit", "-require="+req.Path+"@"+v)
		cmd.Dir = buildDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("align %s@%s failed: %s", req.Path, v, out)
		}
		changed = true
	}

	if changed {
		// 补全新版本的 go.sum 条目。显式 require 是硬约束，tidy 不会降级版本。
		cmd := exec.Command("go", "mod", "tidy")
		cmd.Dir = buildDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go mod tidy after align failed: %s", out)
		}
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
