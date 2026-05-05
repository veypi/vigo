//
// embed.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"fmt"
	"io/fs"
	"time"
)

// embedFS wraps an embed.FS with prefix support
type embedFS struct {
	fs fs.FS
}

// NewEmbedFS creates a read-only file system from an embedded FS.
// Similar to NewLocalFS, it returns only the FS interface.
func NewEmbedFS(efs fs.FS, prefix string) (ReadOnlyFS, error) {
	var root fs.FS = efs
	var err error

	if prefix != "" && prefix != "." {
		root, err = fs.Sub(efs, prefix)
		if err != nil {
			return nil, err
		}
	}

	return &embedFS{fs: root}, nil
}

// ReadFile implements FS.ReadFile
func (f *embedFS) ReadFile(name string) ([]byte, error) {
	name, err := validatePath(name, "read")
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(f.fs, name)
}

// Open implements fs.FS
func (f *embedFS) Open(name string) (fs.File, error) {
	name, err := validatePath(name, "open")
	if err != nil {
		return nil, err
	}
	return f.fs.Open(name)
}

// ReadDir implements fs.ReadDirFS
func (f *embedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	name, err := validatePath(name, "readdir")
	if err != nil {
		return nil, err
	}
	if rdfs, ok := f.fs.(fs.ReadDirFS); ok {
		return rdfs.ReadDir(name)
	}
	// Fallback
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if rdf, ok := file.(fs.ReadDirFile); ok {
		return rdf.ReadDir(-1)
	}
	return nil, fmt.Errorf("file does not support ReadDir: %s", name)
}

// Stat implements fs.StatFS
func (f *embedFS) Stat(name string) (fs.FileInfo, error) {
	name, err := validatePath(name, "stat")
	if err != nil {
		return nil, err
	}
	if sfs, ok := f.fs.(fs.StatFS); ok {
		return sfs.Stat(name)
	}
	// Fallback
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

// generateETag generates an ETag based on file size and modTime.
// Uses zero-cost format: "{modTime}-{size}" for efficient caching without hash computation.
func generateETag(size int64, modTime time.Time) string {
	return fmt.Sprintf(`"%d-%d"`, modTime.Unix(), size)
}
