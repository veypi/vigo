//
// fs.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package doc

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"time"

	"github.com/veypi/vigo/contrib/ufs"
)

// docFileSystem wraps a user-provided fs.FS, injecting an in-memory
// index.html with the router prefix. All file types are exposed.
type docFileSystem struct {
	docFS     fs.FS
	indexHTML string
}

func newDocFS(docFS fs.FS, indexHTML string) *docFileSystem {
	return &docFileSystem{
		docFS:     docFS,
		indexHTML: indexHTML,
	}
}

// Open implements fs.FS
func (f *docFileSystem) Open(name string) (fs.File, error) {
	name = cleanPath(name)
	if name == "index.html" {
		return newMemFile("index.html", []byte(f.indexHTML)), nil
	}
	return f.docFS.Open(name)
}

// ReadDir implements fs.ReadDirFS
func (f *docFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	name = cleanPath(name)
	rawEntries, err := readDir(f.docFS, name)
	if err != nil {
		if name == "." {
			return []fs.DirEntry{newMemDirEntry("index.html", false)}, nil
		}
		return nil, err
	}

	var entries []fs.DirEntry

	// Inject index.html at root
	if name == "." {
		entries = append(entries, newMemDirEntry("index.html", false))
	}

	for _, e := range rawEntries {
		if len(e.Name()) == 0 || e.Name()[0] == '.' {
			continue
		}
		if e.IsDir() {
			if hasFiles(f.docFS, path.Join(name, e.Name())) {
				entries = append(entries, e)
			}
		} else {
			entries = append(entries, e)
		}
	}

	return entries, nil
}

// Stat implements fs.StatFS
func (f *docFileSystem) Stat(name string) (fs.FileInfo, error) {
	name = cleanPath(name)
	if name == "index.html" {
		return newMemFileInfo("index.html", int64(len(f.indexHTML))), nil
	}
	return statFS(f.docFS, name)
}

// ReadFile implements fs.ReadFileFS for optimized file reads.
func (f *docFileSystem) ReadFile(name string) ([]byte, error) {
	name = cleanPath(name)
	if name == "index.html" {
		return []byte(f.indexHTML), nil
	}
	return fs.ReadFile(f.docFS, name)
}

// Search implements ufs.Searcher.
func (f *docFileSystem) Search(searchPath, glob, pattern string, limit int, ignoreCase bool) ([]ufs.SearchMatch, error) {
	searchPath = cleanPath(searchPath)
	if glob == "" {
		glob = "**/*"
	}
	results, err := ufs.Search(f, searchPath, glob, pattern, limit, ignoreCase)
	if err != nil {
		return nil, err
	}
	// Filter out injected index.html from root search results
	if searchPath == "." {
		filtered := results[:0]
		for _, m := range results {
			if m.Path != "index.html" {
				filtered = append(filtered, m)
			}
		}
		return filtered, nil
	}
	return results, nil
}

// hasFiles checks if a directory or any subdirectory contains files.
func hasFiles(fsys fs.FS, dir string) bool {
	entries, err := readDir(fsys, dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			continue
		}
		if e.IsDir() {
			if hasFiles(fsys, path.Join(dir, e.Name())) {
				return true
			}
		} else {
			return true
		}
	}
	return false
}

func cleanPath(name string) string {
	for len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	if name == "" {
		name = "."
	}
	return name
}

// readDir reads directory entries from fsys, preferring fs.ReadDirFS.
func readDir(fsys fs.FS, name string) ([]fs.DirEntry, error) {
	if rdfs, ok := fsys.(fs.ReadDirFS); ok {
		return rdfs.ReadDir(name)
	}
	file, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if rdf, ok := file.(fs.ReadDirFile); ok {
		return rdf.ReadDir(-1)
	}
	return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
}

// statFS returns FileInfo, preferring fs.StatFS.
func statFS(fsys fs.FS, name string) (fs.FileInfo, error) {
	if sfs, ok := fsys.(fs.StatFS); ok {
		return sfs.Stat(name)
	}
	file, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

// memFile implements fs.File and io.ReadSeeker for in-memory content.
type memFile struct {
	name   string
	data   []byte
	offset int64
}

func newMemFile(name string, data []byte) *memFile {
	return &memFile{name: name, data: data}
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return newMemFileInfo(f.name, int64(len(f.data))), nil
}

func (f *memFile) Read(b []byte) (int, error) {
	if f.offset >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(b, f.data[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *memFile) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = f.offset + offset
	case io.SeekEnd:
		abs = int64(len(f.data)) + offset
	}
	if abs < 0 {
		return 0, errors.New("negative position")
	}
	f.offset = abs
	return abs, nil
}

func (f *memFile) Close() error { return nil }

// memFileInfo implements fs.FileInfo
type memFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

func newMemFileInfo(name string, size int64) *memFileInfo {
	return &memFileInfo{name: name, size: size, modTime: time.Time{}}
}

func (i *memFileInfo) Name() string       { return i.name }
func (i *memFileInfo) Size() int64        { return i.size }
func (i *memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i *memFileInfo) ModTime() time.Time { return i.modTime }
func (i *memFileInfo) IsDir() bool        { return i.isDir }
func (i *memFileInfo) Sys() any           { return nil }

// memDirEntry implements fs.DirEntry
type memDirEntry struct {
	name  string
	isDir bool
}

func newMemDirEntry(name string, isDir bool) *memDirEntry {
	return &memDirEntry{name: name, isDir: isDir}
}

func (e *memDirEntry) Name() string               { return e.name }
func (e *memDirEntry) IsDir() bool                 { return e.isDir }
func (e *memDirEntry) Type() fs.FileMode           { return 0 }
func (e *memDirEntry) Info() (fs.FileInfo, error)  {
	return newMemFileInfo(e.name, 0), nil
}
