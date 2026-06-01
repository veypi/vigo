//
// search.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"bufio"
	"bytes"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// matchGlob reports whether name matches the glob pattern.
// It supports ** (matches zero or more path segments), * and ? (single segment only).
// Both pattern and name use "/" as separator.
func matchGlob(pattern, name string) bool {
	patParts := strings.Split(pattern, "/")
	nameParts := strings.Split(name, "/")
	n, m := len(patParts), len(nameParts)

	dp := make([][]bool, n+1)
	for i := range dp {
		dp[i] = make([]bool, m+1)
	}
	dp[0][0] = true

	for i := 1; i <= n && patParts[i-1] == "**"; i++ {
		dp[i][0] = true
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if patParts[i-1] == "**" {
				dp[i][j] = dp[i-1][j] || dp[i][j-1]
			} else {
				matched, _ := path.Match(patParts[i-1], nameParts[j-1])
				dp[i][j] = matched && dp[i-1][j-1]
			}
		}
	}
	return dp[n][m]
}

// skipDirs lists directory names that Grep should not descend into.
// Directories starting with "." are also skipped regardless of this list.
var skipDirs = map[string]bool{
	"node_modules":      true,
	"vendor":            true,
	"__pycache__":       true,
	"bower_components":  true,
	"dist":              true,
	"build":             true,
	"target":            true,
	".next":             true,
	".nuxt":             true,
	"coverage":          true,
	".turbo":            true,
	".output":           true,
}

// SearchGlob walks the filesystem from searchPath and returns files whose
// path relative to searchPath matches the pattern. The pattern supports **
// (zero or more path segments), * and ? (single segment, no path separator).
// Hidden entries (starting with ".") are skipped. Results are sorted by
// modification time descending, then truncated to limit.
func SearchGlob(fsys fs.FS, searchPath, pattern string, limit int) ([]GlobMatch, error) {
	searchPath, err := validatePath(searchPath, "glob")
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	var results []GlobMatch
	err = fs.WalkDir(fsys, searchPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		// Skip hidden entries, but not the root search path itself
		if p != searchPath && len(d.Name()) > 0 && d.Name()[0] == '.' {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Only match files (not directories) against the pattern
		if d.IsDir() {
			return nil
		}
		rel := relPath(searchPath, p)
		if !matchGlob(pattern, rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		results = append(results, GlobMatch{
			Path:    rel,
			IsDir:   false,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].ModTime > results[j].ModTime
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// SearchGrep walks the filesystem from searchPath, filters files by glob (supporting **),
// and searches their content for lines matching the regex pattern.
// Hidden entries, known cache directories, and binary files are skipped.
// When ignoreCase is true, the regex is compiled with case-insensitive flag.
// Results are sorted by file modification time descending, then line number ascending.
func SearchGrep(fsys fs.FS, searchPath, glob, pattern string, limit int, ignoreCase bool) ([]GrepMatch, error) {
	searchPath, err := validatePath(searchPath, "grep")
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	if ignoreCase {
		// Prepend (?i) flag. If pattern already contains (?i), the duplicate
		// is redundant but valid in RE2 — no need for extra detection logic.
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	// Phase 1: collect matching files with their mod time
	type fileMatch struct {
		path    string
		modTime int64
	}
	var files []fileMatch

	err = fs.WalkDir(fsys, searchPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == searchPath {
			return nil
		}
		// Skip hidden entries
		if len(d.Name()) > 0 && d.Name()[0] == '.' {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		// Filter by glob against relative path
		if glob != "" {
			rel := relPath(searchPath, p)
			if !matchGlob(glob, rel) {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, fileMatch{
			path:    p,
			modTime: info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort files by mod time desc (stable for deterministic results)
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	// Phase 2: grep each file for content matches
	var results []GrepMatch
	for _, fm := range files {
		fileResults, err := grepFile(fsys, fm.path, re)
		if err != nil {
			continue
		}
		rel := relPath(searchPath, fm.path)
		for _, r := range fileResults {
			results = append(results, GrepMatch{
				Path:    rel,
				LineNum: r.LineNum,
				Line:    r.Line,
				Column:  r.Column,
			})
			if len(results) >= limit {
				return results[:limit], nil
			}
		}
	}

	return results, nil
}

// grepResult is a raw content match before path resolution.
type grepResult struct {
	LineNum int
	Line    string
	Column  int
}

// grepFile reads a file line by line and returns regex matches.
func grepFile(fsys fs.FS, filePath string, re *regexp.Regexp) ([]grepResult, error) {
	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return nil, err
	}
	if isBinary(data) {
		return nil, nil
	}

	var results []grepResult
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // max 1MB per line
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		locs := re.FindStringIndex(line)
		if locs == nil {
			continue
		}
		results = append(results, grepResult{
			LineNum: lineNum,
			Line:    line,
			Column:  locs[0] + 1, // 1-based
		})
	}
	if err := scanner.Err(); err != nil {
		return results, err
	}
	return results, nil
}

// isBinary checks if data appears to be binary by looking for null bytes
// in the first 8KB.
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	n := min(len(data), 8192)
	return bytes.IndexByte(data[:n], 0) >= 0
}

// relPath computes the relative path of p from base.
// Both use "/" as separator (fs.FS convention).
func relPath(base, p string) string {
	if base == "." || base == "" {
		return p
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if s, ok := strings.CutPrefix(p, base); ok {
		return s
	}
	return p
}
