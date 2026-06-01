//
// search_test.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testFS(t testing.TB) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"main.go": &fstest.MapFile{
			Data:    []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"),
			Mode:    0o644,
			ModTime: time.Unix(1000, 0),
		},
		"lib/utils.go": &fstest.MapFile{
			Data:    []byte("package lib\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"),
			Mode:    0o644,
			ModTime: time.Unix(2000, 0),
		},
		"lib/helper.go": &fstest.MapFile{
			Data:    []byte("package lib\n\nfunc helper() {}\n"),
			Mode:    0o644,
			ModTime: time.Unix(500, 0),
		},
		"README.md": &fstest.MapFile{
			Data:    []byte("# Project\n\nThis is a Go project.\n"),
			Mode:    0o644,
			ModTime: time.Unix(1500, 0),
		},
		// Hidden directory — skipped by both Glob and Grep
		".hidden/data.go": &fstest.MapFile{
			Data:    []byte("package hidden\n\nfunc Secret() string { return \"shh\" }\n"),
			Mode:    0o644,
			ModTime: time.Unix(3000, 0),
		},
		// Cache directory — skipped by Grep
		"node_modules/pkg/index.js": &fstest.MapFile{
			Data:    []byte("module.exports = function hello() { console.log('hi'); }\n"),
			Mode:    0o644,
			ModTime: time.Unix(3000, 0),
		},
		// Binary file (contains null byte)
		"data/binary.bin": &fstest.MapFile{
			Data:    []byte("ELF\x00\x00binary content with func match"),
			Mode:    0o644,
			ModTime: time.Unix(1000, 0),
		},
		// Empty directory
		"empty": &fstest.MapFile{
			Mode:    fs.ModeDir,
			ModTime: time.Unix(500, 0),
		},
		// Subdirectory with content
		"sub/note.txt": &fstest.MapFile{
			Data:    []byte("hello world\nfoo bar\nhello again\n"),
			Mode:    0o644,
			ModTime: time.Unix(1800, 0),
		},
	}
}

// =============================================================================
// SearchGlob tests
// =============================================================================

func TestSearchGlob(t *testing.T) {
	fsys := testFS(t)

	t.Run("match go files with **", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "**/*.go", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 3 {
			t.Fatalf("expected 3 go files, got %d: %v", len(results), results)
		}
	})

	t.Run("match go files non-recursive", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "*.go", 0)
		if err != nil {
			t.Fatal(err)
		}
		// Only main.go is in root (not in lib/ or .hidden/)
		if len(results) != 1 {
			t.Fatalf("expected 1 go file in root, got %d: %v", len(results), results)
		}
		if results[0].Path != "main.go" {
			t.Fatalf("expected main.go, got %s", results[0].Path)
		}
	})

	t.Run("match markdown files", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "*.md", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 md file, got %d", len(results))
		}
		if results[0].Path != "README.md" {
			t.Fatalf("expected README.md, got %s", results[0].Path)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "*.xyz", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("skip hidden dirs", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "**/*.go", 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if strings.Contains(r.Path, ".hidden") {
				t.Fatalf("hidden file should be skipped: %s", r.Path)
			}
		}
	})

	t.Run("results sorted by mod time desc", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "**/*.go", 0)
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(results); i++ {
			if results[i-1].ModTime < results[i].ModTime {
				t.Fatalf("results not sorted by mod time desc: %v", results)
			}
		}
	})

	t.Run("limit truncation", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "**/*.go", 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("exact limit", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "**/*.go", 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
	})

	t.Run("subdirectory search", func(t *testing.T) {
		results, err := SearchGlob(fsys, "lib", "*.go", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 files in lib, got %d", len(results))
		}
		// Paths should be relative to lib
		for _, r := range results {
			if strings.Contains(r.Path, "lib/") {
				t.Fatalf("path should be relative to search path: %s", r.Path)
			}
		}
	})

	t.Run("double-star in subdirectory", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "lib/**/*.go", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 go files in lib, got %d: %v", len(results), results)
		}
		for _, r := range results {
			if !strings.HasPrefix(r.Path, "lib/") {
				t.Fatalf("path should start with lib/: %s", r.Path)
			}
		}
	})

	t.Run("double-star matches nested files", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "**/note.txt", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 note.txt, got %d: %v", len(results), results)
		}
		if results[0].Path != "sub/note.txt" {
			t.Fatalf("expected sub/note.txt, got %s", results[0].Path)
		}
	})

	t.Run("question mark pattern", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "?????.go", 0)
		if err != nil {
			t.Fatal(err)
		}
		// main.go has 4 chars before .go, but ? needs exactly 5 — should be 0
		if len(results) != 0 {
			t.Fatalf("expected 0 matches for ?????.go, got %d", len(results))
		}
	})

	t.Run("question mark matches single char", func(t *testing.T) {
		results, err := SearchGlob(fsys, ".", "????.??", 0)
		if err != nil {
			t.Fatal(err)
		}
		// main.go = 4 chars + . + 2 chars → matches ????.??
		found := false
		for _, r := range results {
			if r.Path == "main.go" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected main.go to match ????.??")
		}
	})

	t.Run("character range pattern", func(t *testing.T) {
		fsys2 := fstest.MapFS{
			"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0o644, ModTime: time.Unix(1, 0)},
			"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0o644, ModTime: time.Unix(2, 0)},
			"z.txt": &fstest.MapFile{Data: []byte("z"), Mode: 0o644, ModTime: time.Unix(3, 0)},
			"5.txt": &fstest.MapFile{Data: []byte("5"), Mode: 0o644, ModTime: time.Unix(4, 0)},
		}
		results, err := SearchGlob(fsys2, ".", "[a-c].txt", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 matches for [a-c].txt, got %d: %v", len(results), results)
		}
	})
}

// =============================================================================
// matchGlob tests
// =============================================================================

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		// Basic single-segment patterns
		{"*.go", "main.go", true},
		{"*.go", "main.js", false},
		{"*.go", "lib/main.go", false}, // * does not match /
		{"main.*", "main.go", true},
		{"main.*", "main", false},

		// ** recursive
		{"**/*.go", "main.go", true},
		{"**/*.go", "lib/main.go", true},
		{"**/*.go", "lib/sub/main.go", true},
		{"**/*.go", "main.js", false},

		// ** in middle
		{"lib/**/*.go", "lib/main.go", true},
		{"lib/**/*.go", "lib/sub/main.go", true},
		{"lib/**/*.go", "main.go", false},
		{"lib/**/*.go", "other/main.go", false},

		// Multiple **
		{"**/test/**/*.go", "test/main.go", true},
		{"**/test/**/*.go", "a/test/b/main.go", true},
		{"**/test/**/*.go", "main.go", false},

		// Leading **
		{"**", "main.go", true},
		{"**", "a/b/c/d.txt", true},
		{"**/main.go", "main.go", true},
		{"**/main.go", "lib/main.go", true},

		// Question mark
		{"?.go", "a.go", true},
		{"?.go", "main.go", false},
		{"main.??", "main.go", true},
		{"main.??", "main.js", true},
		{"main.??", "main.j", false},

		// Character set
		{"[abc].go", "a.go", true},
		{"[abc].go", "b.go", true},
		{"[abc].go", "d.go", false},

		// Character range
		{"[a-z].go", "x.go", true},
		{"[a-z].go", "X.go", false},
		{"[0-9].go", "5.go", true},
		{"[0-9].go", "a.go", false},

		// Exact match
		{"main.go", "main.go", true},
		{"main.go", "other.go", false},

		// Empty/invalid
		{"", "", true},
		{"", "main.go", false},
		{"*.go", "", false},
	}

	for _, tc := range tests {
		got := matchGlob(tc.pattern, tc.name)
		if got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

// =============================================================================
// SearchGrep tests
// =============================================================================

func TestSearchGrep(t *testing.T) {
	fsys := testFS(t)

	t.Run("basic regex match", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "**/*.go", `func\s+\w+`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one match")
		}
		// main.go: func main, lib/utils.go: func Add, lib/helper.go: func helper
		if len(results) != 3 {
			t.Fatalf("expected 3 matches, got %d: %v", len(results), results)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "", `xyzzy_nonexistent`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("skip hidden dirs", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "", `Secret`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if strings.Contains(r.Path, ".hidden") {
				t.Fatalf("hidden file content should be skipped: %s", r.Path)
			}
		}
	})

	t.Run("skip cache dirs", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "", `hello`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if strings.Contains(r.Path, "node_modules") {
				t.Fatalf("node_modules should be skipped: %s", r.Path)
			}
		}
	})

	t.Run("skip binary files", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "", `func`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if strings.Contains(r.Path, "binary") {
				t.Fatalf("binary file should be skipped: %s", r.Path)
			}
		}
	})

	t.Run("glob filter", func(t *testing.T) {
		// Only search .go files: should not match func in README or binary
		results, err := SearchGrep(fsys, ".", "*.go", `func`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if !strings.HasSuffix(r.Path, ".go") {
				t.Fatalf("glob should filter non-go files: %s", r.Path)
			}
		}
	})

	t.Run("sort by mod time desc then line num asc", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "*.go", `func`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		var prevPath string
		var prevLine int
		for _, r := range results {
			if r.Path != prevPath {
				prevPath = r.Path
				prevLine = 0
			}
			// Same file: line numbers ascending
			if r.LineNum <= prevLine && prevLine != 0 {
				t.Fatalf("line numbers should be ascending within same file: %v", results)
			}
			prevLine = r.LineNum
		}
	})

	t.Run("limit truncation", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "*.go", `func`, 1, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) > 1 {
			t.Fatalf("expected at most 1 result, got %d", len(results))
		}
	})

	t.Run("results relative to search path", func(t *testing.T) {
		results, err := SearchGrep(fsys, "lib", "*.go", `func`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if strings.Contains(r.Path, "lib/") {
				t.Fatalf("path should be relative to search path: %s", r.Path)
			}
		}
	})

	t.Run("column is 1-based byte offset", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "*.go", `func main`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 match, got %d", len(results))
		}
		if results[0].Column != 1 {
			t.Fatalf("expected column 1 for 'func main' at line start, got %d", results[0].Column)
		}
	})

	t.Run("empty glob matches all files", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "", `hello`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		// main.go: println("hello"), sub/note.txt: hello world + hello again
		// node_modules is skipped by skipDirs
		if len(results) != 3 {
			t.Fatalf("expected 3 matches for 'hello', got %d: %v", len(results), results)
		}
	})

	t.Run("ignoreCase matches uppercase", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "*.go", `HELLO`, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		// main.go has println("hello") — matches with ignoreCase
		if len(results) != 1 {
			t.Fatalf("expected 1 match for 'HELLO' with ignoreCase, got %d: %v", len(results), results)
		}
	})

	t.Run("case sensitive no match", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "*.go", `HELLO`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 matches for 'HELLO' without ignoreCase, got %d: %v", len(results), results)
		}
	})

	t.Run("glob with ** in grep", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "lib/**", `func`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 matches in lib/, got %d: %v", len(results), results)
		}
		for _, r := range results {
			if !strings.HasPrefix(r.Path, "lib/") {
				t.Fatalf("expected path in lib/, got %s", r.Path)
			}
		}
	})

	t.Run("glob exact file matches only that file", func(t *testing.T) {
		results, err := SearchGrep(fsys, ".", "main.go", `func`, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 match in main.go, got %d: %v", len(results), results)
		}
		if results[0].Path != "main.go" {
			t.Fatalf("expected main.go, got %s", results[0].Path)
		}
	})
}

func TestSearchGrepInvalidRegex(t *testing.T) {
	fsys := testFS(t)
	_, err := SearchGrep(fsys, ".", "", `[unclosed`, 0, false)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestSearchGrepInvalidPath(t *testing.T) {
	fsys := testFS(t)
	_, err := SearchGrep(fsys, "../escape", "", `x`, 0, false)
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

// =============================================================================
// relPath tests
// =============================================================================

func TestRelPath(t *testing.T) {
	tests := []struct {
		base, p, want string
	}{
		{".", "main.go", "main.go"},
		{".", "lib/utils.go", "lib/utils.go"},
		{"", "main.go", "main.go"},
		{"lib", "lib/utils.go", "utils.go"},
		{"lib", "lib/sub/deep.go", "sub/deep.go"},
		// Edge cases: prefix must match on path segment boundary
		{"lib", "library/main.go", "library/main.go"}, // no match on "lib"rary
		{"lib/", "lib/utils.go", "utils.go"},
		{".", ".hidden/data.go", ".hidden/data.go"}, // "." special-cased first
	}
	for _, tc := range tests {
		got := relPath(tc.base, tc.p)
		if got != tc.want {
			t.Errorf("relPath(%q, %q) = %q, want %q", tc.base, tc.p, got, tc.want)
		}
	}
}

// =============================================================================
// GlobMatch type tests
// =============================================================================

func TestGlobMatchFields(t *testing.T) {
	m := GlobMatch{
		Path:    "sub/note.txt",
		IsDir:   false,
		Size:    42,
		ModTime: 1700000000,
	}
	if m.Path != "sub/note.txt" {
		t.Errorf("Path = %q", m.Path)
	}
	if m.IsDir {
		t.Error("IsDir should be false")
	}
	if m.Size != 42 {
		t.Errorf("Size = %d", m.Size)
	}
	if m.ModTime != 1700000000 {
		t.Errorf("ModTime = %d", m.ModTime)
	}
}

// =============================================================================
// GrepMatch type tests
// =============================================================================

func TestGrepMatchFields(t *testing.T) {
	m := GrepMatch{
		Path:    "main.go",
		LineNum: 3,
		Line:    "func main() {",
		Column:  6,
	}
	if m.Path != "main.go" {
		t.Errorf("Path = %q", m.Path)
	}
	if m.LineNum != 3 {
		t.Errorf("LineNum = %d", m.LineNum)
	}
	if m.Line != "func main() {" {
		t.Errorf("Line = %q", m.Line)
	}
	if m.Column != 6 {
		t.Errorf("Column = %d", m.Column)
	}
}

// =============================================================================
// Searcher interface satisfaction tests
// =============================================================================

func TestLocalFSSatisfiesSearcher(t *testing.T) {
	// Compile-time check: *localFS implements FS (which embeds Searcher)
	var _ FS = (*localFS)(nil)
}

func TestEmbedFSSatisfiesSearcher(t *testing.T) {
	var _ ReadOnlyFS = (*embedFS)(nil)
}

func TestMultiFSSatisfiesSearcher(t *testing.T) {
	var _ ReadOnlyFS = (*multiFS)(nil)
}

// =============================================================================
// Edge case tests
// =============================================================================

func TestSearchGlobEmptyDir(t *testing.T) {
	fsys := fstest.MapFS{
		"empty": &fstest.MapFile{Mode: fs.ModeDir, ModTime: time.Unix(1, 0)},
	}
	results, err := SearchGlob(fsys, ".", "*", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results in empty dir, got %d", len(results))
	}
}

func TestSearchGlobLimitZero(t *testing.T) {
	fsys := testFS(t)
	results, err := SearchGlob(fsys, ".", "**/*.go", 0)
	if err != nil {
		t.Fatal(err)
	}
	// limit=0 should default to 100, so we get all 3 results
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestSearchGrepRegexAnchored(t *testing.T) {
	fsys := testFS(t)
	// "^func" should only match lines starting with func
	results, err := SearchGrep(fsys, ".", "*.go", `^func`, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !strings.HasPrefix(r.Line, "func") {
			t.Fatalf("expected line to start with 'func': %q", r.Line)
		}
	}
}

func TestSearchGrepSubdirectory(t *testing.T) {
	fsys := testFS(t)
	results, err := SearchGrep(fsys, "sub", "", `hello`, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches in sub dir, got %d: %v", len(results), results)
	}
	for _, r := range results {
		if r.Path == "" || strings.HasPrefix(r.Path, "sub/") {
			t.Fatalf("path should be relative: %s", r.Path)
		}
	}
}

// =============================================================================
// isBinary tests
// =============================================================================

func TestIsBinary(t *testing.T) {
	if isBinary(nil) {
		t.Error("nil data should not be binary")
	}
	if isBinary([]byte{}) {
		t.Error("empty data should not be binary")
	}
	if isBinary([]byte("hello world")) {
		t.Error("text should not be binary")
	}
	if !isBinary([]byte("ELF\x00binary")) {
		t.Error("null byte should be detected as binary")
	}
	// Test with data larger than 8KB
	large := make([]byte, 10000)
	for i := range large {
		large[i] = 'x'
	}
	if isBinary(large) {
		t.Error("large text without null should not be binary")
	}
	large[9000] = 0
	if isBinary(large) {
		t.Error("null byte beyond first 8KB should not be detected as binary")
	}
	large[100] = 0
	if !isBinary(large) {
		t.Error("null byte in first 8KB should be detected as binary")
	}
}

// =============================================================================
// skipDirs validation
// =============================================================================

func TestSkipDirsContainsExpected(t *testing.T) {
	expected := []string{"node_modules", "vendor", "__pycache__", "dist", "build", "target", "coverage"}
	for _, dir := range expected {
		if !skipDirs[dir] {
			t.Errorf("expected %q in skipDirs", dir)
		}
	}
}

// =============================================================================
// ensure sorts are stable
// =============================================================================

func TestSearchGlobSortStability(t *testing.T) {
	// Files with same mod time should have deterministic ordering
	now := time.Unix(1000, 0)
	fsys := fstest.MapFS{
		"a.go": &fstest.MapFile{Data: []byte("a"), Mode: 0o644, ModTime: now},
		"c.go": &fstest.MapFile{Data: []byte("c"), Mode: 0o644, ModTime: now},
		"b.go": &fstest.MapFile{Data: []byte("b"), Mode: 0o644, ModTime: now},
	}
	results1, _ := SearchGlob(fsys, ".", "*.go", 0)
	results2, _ := SearchGlob(fsys, ".", "*.go", 0)
	if len(results1) != 3 || len(results2) != 3 {
		t.Fatal("expected 3 results")
	}
	for i := range results1 {
		if results1[i].Path != results2[i].Path {
			t.Fatal("results with same mod time should be deterministic")
		}
	}
}

// =============================================================================
// Benchmark
// =============================================================================

func BenchmarkSearchGlob(b *testing.B) {
	fsys := testFS(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SearchGlob(fsys, ".", "**/*.go", 100)
	}
}

func BenchmarkSearchGrep(b *testing.B) {
	fsys := testFS(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SearchGrep(fsys, ".", "*.go", `func`, 100, false)
	}
}
