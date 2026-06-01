//
// types.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"io"
	"io/fs"
	"os"
)

// File 统一的文件接口（支持读写）
type File interface {
	fs.File
	io.Writer
	io.Seeker
}

// FS 可写文件系统接口
type FS interface {
	fs.FS
	fs.ReadDirFS
	fs.StatFS
	Searcher

	// ReadFile reads the named file and returns its contents.
	ReadFile(name string) ([]byte, error)
	// Create creates or truncates the named file.
	Create(name string) (File, error)
	// MkdirAll creates a directory named path, along with any necessary parents.
	MkdirAll(path string, perm os.FileMode) error
	// RemoveAll removes path and any children it contains.
	RemoveAll(path string) error
	// Rename renames (moves) oldname to newname.
	Rename(oldname, newname string) error
	// WriteFile writes data to the named file, creating it if necessary.
	WriteFile(name string, data []byte, perm fs.FileMode) error
}

// ItemEntry describes a file or directory in a JSON directory listing.
// Directories include an Items field with their children, allowing recursive nesting.
type ItemEntry struct {
	Name    string      `json:"name"`
	Dir     bool        `json:"dir"`
	Size    int64       `json:"size"`
	Mime    string      `json:"mime"`
	ModTime int64       `json:"mod_time"`
	IsRepo  bool        `json:"is_repo"`
	Items   []ItemEntry `json:"items,omitempty"`
}

// GlobMatch describes a file matching a glob pattern.
type GlobMatch struct {
	Path    string `json:"path"` // relative to search path
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"` // unix timestamp
}

// GrepMatch describes a content match.
type GrepMatch struct {
	Path    string `json:"path"`     // relative to search path
	LineNum int    `json:"line_num"` // 1-based line number
	Line    string `json:"line"`     // matched line content
	Column  int    `json:"column"`   // 1-based byte offset of match start
}

// Searcher provides file search and content search capabilities.
type Searcher interface {
	// Glob searches for files under path whose relative path matches pattern.
	//
	// Pattern syntax (matches against relative path, "/" as separator):
	//   - **        matches zero or more path segments (e.g., **/*.go, lib/**)
	//   - *         matches any sequence of non-separator characters within a single segment
	//   - ?         matches any single non-separator character
	//   - [abc]     matches any single character in the set
	//   - [a-z]     matches any single character in the range
	//   - exact     literal path component match (e.g., main.go matches only main.go)
	//
	// Examples:
	//   **/*.go       recursively match all .go files
	//   *.go          match .go files only in the root (non-recursive)
	//   lib/**/*.go   match .go files anywhere under lib/
	//   **/test/*.go  match .go files in any directory named "test"
	//
	// Hidden entries (starting with ".") are skipped.
	// Results are sorted by modification time descending.
	Glob(path, pattern string, limit int) ([]GlobMatch, error)

	// Grep searches file contents under path. Files are filtered by glob
	// (same pattern syntax as Glob), then each line is matched against
	// the regex pattern.
	//
	// glob   — file path filter, same syntax as Glob.pattern (supports **, *, ?, [a-z]).
	//          Empty string matches all non-hidden files.
	// pattern — regexp (RE2) matched against each line of file content.
	//          When ignoreCase is true, the regex is compiled with (?i) case-insensitive flag.
	//
	// Skipped content:
	//   - hidden entries (names starting with ".")
	//   - directories in skipDirs list (node_modules, vendor, dist, build, etc.)
	//   - binary files (detected by null byte in first 8KB)
	//
	// Results are sorted by file modification time descending, then line number ascending.
	Grep(path, glob, pattern string, limit int, ignoreCase bool) ([]GrepMatch, error)
}

// ReadOnlyFS 只读文件系统接口
type ReadOnlyFS interface {
	fs.FS
	fs.ReadDirFS
	fs.StatFS
	Searcher

	// ReadFile reads the named file and returns its contents.
	ReadFile(name string) ([]byte, error)
}
