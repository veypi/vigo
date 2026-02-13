package ufs

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/logv"
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

// NewHandler returns a vigo handler that serves static files from the FS.
// It expects a path parameter named "path" in the route (e.g., "/static/{path:*}").
// If the path resolves to a directory, it returns a JSON list of the directory contents.
func (f *FS) NewHandler() func(*vigo.X) {
	return func(x *vigo.X) {
		p := x.PathParams.Get("path")
		if p == "" || p == "/" {
			p = "."
		} else if len(p) > 0 && p[0] == '/' {
			p = p[1:]
		}

		file, err := f.Open(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				x.WriteHeader(http.StatusNotFound)
				return
			}
			if errors.Is(err, fs.ErrPermission) {
				x.WriteHeader(http.StatusForbidden)
				return
			}
			x.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			x.WriteHeader(http.StatusInternalServerError)
			return
		}

		if stat.IsDir() {
			entries, err := f.ReadDir(p)
			if err != nil {
				x.WriteHeader(http.StatusInternalServerError)
				return
			}

			type FileInfo struct {
				Name string `json:"name"`
				Dir  bool   `json:"dir"`
				Size int64  `json:"size"`
			}

			list := make([]FileInfo, 0, len(entries))
			for _, e := range entries {
				info, err := e.Info()
				size := int64(0)
				if err == nil {
					size = info.Size()
				}
				list = append(list, FileInfo{
					Name: e.Name(),
					Dir:  e.IsDir(),
					Size: size,
				})
			}
			x.JSON(list)
			return
		}

		if rs, ok := file.(io.ReadSeeker); ok {
			http.ServeContent(x.ResponseWriter(), x.Request, stat.Name(), stat.ModTime(), rs)
			return
		}
		x.WriteHeader(http.StatusInternalServerError)
	}
}

// NewHandlerWithDefaultFile returns a vigo handler with a default file fallback.
// If the requested path is not found or is a directory, the default file is served.
// The default file is loaded into memory during initialization.
func (f *FS) NewHandlerWithDefaultFile(defaultFile string) func(*vigo.X) {
	var defaultContent []byte
	var defaultModTime time.Time
	var defaultName string

	df, err := f.Open(defaultFile)
	if err != nil {
		logv.Warn().Err(err).Msgf("default file %s not found", defaultFile)
	} else {
		stat, _ := df.Stat()
		if stat != nil {
			defaultModTime = stat.ModTime()
			defaultName = stat.Name()
		}
		content, err := io.ReadAll(df)
		df.Close()
		if err != nil {
			logv.Warn().Err(err).Msgf("failed to read default file %s", defaultFile)
		} else {
			defaultContent = content
		}
	}

	return func(x *vigo.X) {
		p := x.PathParams.Get("path")
		if p == "" || p == "/" {
			p = "."
		} else if len(p) > 0 && p[0] == '/' {
			p = p[1:]
		}

		file, err := f.Open(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && defaultContent != nil {
				// If path has extension, return 404
				if path.Ext(p) != "" {
					x.WriteHeader(http.StatusNotFound)
					return
				}
				http.ServeContent(x.ResponseWriter(), x.Request, defaultName, defaultModTime, bytes.NewReader(defaultContent))
				return
			}
			if errors.Is(err, fs.ErrNotExist) {
				x.WriteHeader(http.StatusNotFound)
				return
			}
			if errors.Is(err, fs.ErrPermission) {
				x.WriteHeader(http.StatusForbidden)
				return
			}
			x.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			x.WriteHeader(http.StatusInternalServerError)
			return
		}

		if stat.IsDir() {
			if defaultContent != nil {
				// If path has extension, do not return default file.
				// However, directories usually don't have extensions.
				// If a directory happens to have an extension (e.g. /style.css/), we probably shouldn't serve default file if it looks like a file.
				http.ServeContent(x.ResponseWriter(), x.Request, defaultName, defaultModTime, bytes.NewReader(defaultContent))
				return
			}

			entries, err := f.ReadDir(p)
			if err != nil {
				x.WriteHeader(http.StatusInternalServerError)
				return
			}

			type FileInfo struct {
				Name string `json:"name"`
				Dir  bool   `json:"dir"`
				Size int64  `json:"size"`
			}

			list := make([]FileInfo, 0, len(entries))
			for _, e := range entries {
				info, err := e.Info()
				size := int64(0)
				if err == nil {
					size = info.Size()
				}
				list = append(list, FileInfo{
					Name: e.Name(),
					Dir:  e.IsDir(),
					Size: size,
				})
			}
			x.JSON(list)
			return
		}

		if rs, ok := file.(io.ReadSeeker); ok {
			http.ServeContent(x.ResponseWriter(), x.Request, stat.Name(), stat.ModTime(), rs)
			return
		}
		x.WriteHeader(http.StatusInternalServerError)
	}
}
