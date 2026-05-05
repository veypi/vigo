//
// multi.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"errors"
	"io/fs"
	"sort"
)

// multiFS implements a read-only union file system that searches through layers in order
type multiFS struct {
	layers []fs.FS
}

// NewMultiFS creates a read-only union file system from multiple layers.
// Layers are searched in order, first match wins.
func NewMultiFS(layers ...fs.FS) ReadOnlyFS {
	// Filter out nil layers
	validLayers := make([]fs.FS, 0, len(layers))
	for _, l := range layers {
		if l != nil {
			validLayers = append(validLayers, l)
		}
	}
	return &multiFS{layers: validLayers}
}

// ReadFile implements FS.ReadFile
func (f *multiFS) ReadFile(name string) ([]byte, error) {
	name, err := validatePath(name, "read")
	if err != nil {
		return nil, err
	}
	for _, layer := range f.layers {
		data, err := fs.ReadFile(layer, name)
		if err == nil {
			return data, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
}

// Open implements fs.FS
func (f *multiFS) Open(name string) (fs.File, error) {
	name, err := validatePath(name, "open")
	if err != nil {
		return nil, err
	}

	var firstNotExistErr error
	for _, layer := range f.layers {
		file, err := layer.Open(name)
		if err == nil {
			return file, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			if firstNotExistErr == nil {
				firstNotExistErr = err
			}
			continue
		}
		// Return other errors immediately (e.g., permission denied)
		return nil, err
	}

	if firstNotExistErr != nil {
		return nil, firstNotExistErr
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadDir implements fs.ReadDirFS
func (f *multiFS) ReadDir(name string) ([]fs.DirEntry, error) {
	name, err := validatePath(name, "readdir")
	if err != nil {
		return nil, err
	}

	var firstNotExistErr error
	for _, layer := range f.layers {
		// Try fs.ReadDirFS interface first
		if rdfs, ok := layer.(fs.ReadDirFS); ok {
			entries, err := rdfs.ReadDir(name)
			if err == nil {
				return entries, nil
			}
			if errors.Is(err, fs.ErrNotExist) {
				if firstNotExistErr == nil {
					firstNotExistErr = err
				}
				continue
			}
			return nil, err
		}

		// Fallback to Open and ReadDir
		file, err := layer.Open(name)
		if err == nil {
			var entries []fs.DirEntry
			var readErr error
			if rdf, ok := file.(fs.ReadDirFile); ok {
				entries, readErr = rdf.ReadDir(-1)
			} else {
				stat, sErr := file.Stat()
				if sErr == nil && !stat.IsDir() {
					readErr = &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
				} else {
					readErr = &fs.PathError{Op: "readdir", Path: name, Err: errors.New("file does not implement ReadDirFile")}
				}
			}
			file.Close()

			if readErr == nil {
				return entries, nil
			}
			return nil, readErr
		}

		if errors.Is(err, fs.ErrNotExist) {
			if firstNotExistErr == nil {
				firstNotExistErr = err
			}
			continue
		}
		return nil, err
	}

	if firstNotExistErr != nil {
		return nil, firstNotExistErr
	}
	return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
}

// Stat implements fs.StatFS
func (f *multiFS) Stat(name string) (fs.FileInfo, error) {
	name, err := validatePath(name, "stat")
	if err != nil {
		return nil, err
	}

	var firstNotExistErr error
	for _, layer := range f.layers {
		if sfs, ok := layer.(fs.StatFS); ok {
			info, err := sfs.Stat(name)
			if err == nil {
				return info, nil
			}
			if errors.Is(err, fs.ErrNotExist) {
				if firstNotExistErr == nil {
					firstNotExistErr = err
				}
				continue
			}
			return nil, err
		}

		// Fallback to Open and Stat
		file, err := layer.Open(name)
		if err == nil {
			info, sErr := file.Stat()
			file.Close()
			if sErr == nil {
				return info, nil
			}
			return nil, sErr
		}
		if errors.Is(err, fs.ErrNotExist) {
			if firstNotExistErr == nil {
				firstNotExistErr = err
			}
			continue
		}
		return nil, err
	}

	if firstNotExistErr != nil {
		return nil, firstNotExistErr
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// mergeDirEntries merges directory entries from multiple layers
// First layer wins for duplicate names
func mergeDirEntries(layers [][]fs.DirEntry) []fs.DirEntry {
	seen := make(map[string]bool)
	var result []fs.DirEntry

	for _, layer := range layers {
		for _, entry := range layer {
			name := entry.Name()
			if !seen[name] {
				seen[name] = true
				result = append(result, entry)
			}
		}
	}

	// Sort for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})

	return result
}
