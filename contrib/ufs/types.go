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

// FileEntry describes a file or directory in a JSON directory listing.
type FileEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Mime    string `json:"mime"`
	ModTime int64  `json:"mod_time"`
}

// ReadOnlyFS 只读文件系统接口
type ReadOnlyFS interface {
	fs.FS
	fs.ReadDirFS
	fs.StatFS

	// ReadFile reads the named file and returns its contents.
	ReadFile(name string) ([]byte, error)
}
