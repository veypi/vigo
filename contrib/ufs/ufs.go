package ufs

import (
	"errors"
	"io/fs"
	"os"
)

// FS implements a union file system that searches through layers in order.
// It implements fs.FS, fs.ReadDirFS, and fs.StatFS.
type FS struct {
	layers []fs.FS
}

// EmbedOpt is a helper struct to pass fs.FS (including embed.FS) with a prefix to New.
type EmbedOpt struct {
	FS     fs.FS
	Prefix string
}

// Embed creates an EmbedOpt.
func Embed(efs fs.FS, prefix string) EmbedOpt {
	return EmbedOpt{FS: efs, Prefix: prefix}
}

// New creates a new FS instance.
// Supported arguments:
// - string: treated as a local directory path (uses os.DirFS)
// - fs.FS: added directly
// - EmbedOpt: treated as embed.FS with prefix (uses fs.Sub)
func New(layers ...any) *FS {
	f := &FS{
		layers: make([]fs.FS, 0, len(layers)),
	}
	for _, l := range layers {
		switch v := l.(type) {
		case string:
			f.layers = append(f.layers, os.DirFS(v))
		case fs.FS:
			f.layers = append(f.layers, v)
		case EmbedOpt:
			sub, err := fs.Sub(v.FS, v.Prefix)
			if err == nil {
				f.layers = append(f.layers, sub)
			}
		}
	}
	return f
}

// Open implements fs.FS.
// It searches for the file in each layer in order and returns the first one found.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
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

// ReadDir implements fs.ReadDirFS.
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
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
				// File opened but doesn't support ReadDir, maybe it's not a directory or doesn't implement interface
				// Check if it is a directory
				stat, sErr := file.Stat()
				if sErr == nil && !stat.IsDir() {
					// Not a directory, treat as NotExist for ReadDir purpose or specific error?
					// Standard fs.ReadDir returns error if path is not a dir.
					readErr = &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
				} else {
					readErr = &fs.PathError{Op: "readdir", Path: name, Err: errors.New("file does not implement ReadDirFile")}
				}
			}
			file.Close()

			if readErr == nil {
				return entries, nil
			}
			// If error is NotExist, continue. If not directory, maybe continue?
			// User said "first matching returns". If we matched a file but we wanted ReadDir, it's a match but an error.
			// So we should return error.
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

// Stat implements fs.StatFS.
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
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
