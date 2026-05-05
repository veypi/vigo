//
// local.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// validatePath normalizes a path and validates it.
// Leading "/" are stripped, "" and "/" become ".".
// Returns the cleaned path, or an *fs.PathError if the path is invalid.
func validatePath(name, op string) (string, error) {
	for len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	if name == "" {
		name = "."
	}
	if !fs.ValidPath(name) {
		return name, &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	return name, nil
}

// fsErr extracts the underlying error from OS errors and wraps it in *fs.PathError
// so that the virtual path is exposed instead of the real filesystem path.
func fsErr(err error, op, name string) error {
	if err == nil {
		return nil
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: op, Path: name, Err: pe.Err}
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return &fs.PathError{Op: op, Path: name, Err: le.Err}
	}
	return err
}

// localFS implements FS interface for local file system
type localFS struct {
	root string
}

// NewLocalFS creates a new local file system with full read-write support
func NewLocalFS(root string) (FS, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	return &localFS{root: root}, nil
}

func (f *localFS) Open(name string) (fs.File, error) {
	name, err := validatePath(name, "open")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(f.root, name))
	return file, fsErr(err, "open", name)
}

func (f *localFS) ReadFile(name string) ([]byte, error) {
	name, err := validatePath(name, "read")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(f.root, name))
	return data, fsErr(err, "read", name)
}

func (f *localFS) ReadDir(name string) ([]fs.DirEntry, error) {
	name, err := validatePath(name, "readdir")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(f.root, name))
	return entries, fsErr(err, "readdir", name)
}

func (f *localFS) Stat(name string) (fs.FileInfo, error) {
	name, err := validatePath(name, "stat")
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(filepath.Join(f.root, name))
	return info, fsErr(err, "stat", name)
}

func (f *localFS) Create(name string) (File, error) {
	name, err := validatePath(name, "create")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fsErr(err, "create", name)
	}
	file, err := os.Create(path)
	return file, fsErr(err, "create", name)
}

func (f *localFS) MkdirAll(path string, perm os.FileMode) error {
	path, err := validatePath(path, "mkdir")
	if err != nil {
		return err
	}
	return fsErr(os.MkdirAll(filepath.Join(f.root, path), perm), "mkdir", path)
}

func (f *localFS) RemoveAll(path string) error {
	path, err := validatePath(path, "remove")
	if err != nil {
		return err
	}
	return fsErr(os.RemoveAll(filepath.Join(f.root, path)), "remove", path)
}

func (f *localFS) Rename(oldname, newname string) error {
	oldname, err := validatePath(oldname, "rename")
	if err != nil {
		return err
	}
	newname, err = validatePath(newname, "rename")
	if err != nil {
		return err
	}
	oldPath := filepath.Join(f.root, oldname)
	newPath := filepath.Join(f.root, newname)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fsErr(err, "rename", oldname)
	}
	return fsErr(os.Rename(oldPath, newPath), "rename", oldname)
}

func (f *localFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	name, err := validatePath(name, "write")
	if err != nil {
		return err
	}
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fsErr(err, "write", name)
	}
	return fsErr(os.WriteFile(path, data, perm), "write", name)
}
