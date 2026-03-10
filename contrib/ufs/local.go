//
// local.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"io/fs"
	"os"
	"path/filepath"
)

// localFS implements FS interface for local file system
type localFS struct {
	root string
}

// NewLocalFS creates a new local file system with full read-write support
func NewLocalFS(root string) (FS, error) {
	// Ensure root directory exists
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	return &localFS{root: root}, nil
}

// Open implements fs.FS
func (f *localFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return os.Open(filepath.Join(f.root, name))
}

// ReadDir implements fs.ReadDirFS
func (f *localFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	return os.ReadDir(filepath.Join(f.root, name))
}

// Stat implements fs.StatFS
func (f *localFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	return os.Stat(filepath.Join(f.root, name))
}

// Create creates or truncates the named file
func (f *localFS) Create(name string) (File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "create", Path: name, Err: fs.ErrInvalid}
	}
	path := filepath.Join(f.root, name)
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

// MkdirAll creates a directory named path, along with any necessary parents
func (f *localFS) MkdirAll(path string, perm os.FileMode) error {
	if !fs.ValidPath(path) {
		return &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrInvalid}
	}
	return os.MkdirAll(filepath.Join(f.root, path), perm)
}

// RemoveAll removes path and any children it contains
func (f *localFS) RemoveAll(path string) error {
	if !fs.ValidPath(path) {
		return &fs.PathError{Op: "remove", Path: path, Err: fs.ErrInvalid}
	}
	return os.RemoveAll(filepath.Join(f.root, path))
}

// Rename renames (moves) oldname to newname
func (f *localFS) Rename(oldname, newname string) error {
	if !fs.ValidPath(oldname) {
		return &fs.PathError{Op: "rename", Path: oldname, Err: fs.ErrInvalid}
	}
	if !fs.ValidPath(newname) {
		return &fs.PathError{Op: "rename", Path: newname, Err: fs.ErrInvalid}
	}
	oldPath := filepath.Join(f.root, oldname)
	newPath := filepath.Join(f.root, newname)
	// Ensure parent directory of newname exists
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// WriteFile writes data to the named file, creating it if necessary
func (f *localFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: "write", Path: name, Err: fs.ErrInvalid}
	}
	path := filepath.Join(f.root, name)
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}
