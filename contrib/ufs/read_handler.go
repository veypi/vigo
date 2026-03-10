//
// read_handler.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"time"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/logv"
)

// HandlerOptions configures the HTTP handler behavior
type HandlerOptions struct {
	// CacheControl sets the Cache-Control header
	// Default: "public, max-age=0, must-revalidate"
	CacheControl string

	// ETagCache is a map of pre-computed ETags (path -> etag)
	// Used by EmbedFS to avoid runtime ETag calculation
	ETagCache map[string]string
}

// DefaultHandlerOptions returns default options
func DefaultHandlerOptions() *HandlerOptions {
	return &HandlerOptions{
		CacheControl: "public, max-age=0, must-revalidate",
		ETagCache:    nil,
	}
}

// mergeOptions merges provided options with defaults
func mergeOptions(opts []*HandlerOptions) *HandlerOptions {
	result := DefaultHandlerOptions()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if opt.CacheControl != "" {
			result.CacheControl = opt.CacheControl
		}
		if opt.ETagCache != nil {
			result.ETagCache = opt.ETagCache
		}
	}
	return result
}

// getETag returns the ETag for a file, using cache if available
func getETag(filePath string, info fs.FileInfo, cache map[string]string) string {
	if cache != nil {
		if etag, ok := cache[filePath]; ok {
			return etag
		}
	}
	return generateETag(info.Size(), info.ModTime())
}

// checkNotModified checks if the request has matching If-None-Match or If-Modified-Since headers
// Returns true if the response should be 304 Not Modified
func checkNotModified(r *http.Request, etag string, modTime time.Time) bool {
	// Check If-None-Match (ETag) - takes precedence
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return inm == etag || inm == "*"
	}

	// Check If-Modified-Since
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if imsTime, err := http.ParseTime(ims); err == nil {
			return !modTime.After(imsTime)
		}
	}

	return false
}

// setCacheHeaders sets common cache-related headers
func setCacheHeaders(w http.ResponseWriter, etag string, modTime time.Time, cacheControl string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", cacheControl)
}

// NewHandler returns a vigo handler that serves static files from the FS.
// It expects a path parameter named "path" in the route (e.g., "/static/{path:*}").
// If the path resolves to a directory, it returns a JSON list of the directory contents.
//
// HTTP Cache Support:
//   - ETag: Generated from file size and modification time (or from ETagCache if provided)
//   - Last-Modified: File modification time
//   - 304 Not Modified: Returned when If-None-Match or If-Modified-Since match
//   - Cache-Control: Configurable via HandlerOptions
func NewHandler(filesystem fs.FS, opts ...*HandlerOptions) func(*vigo.X) {
	options := mergeOptions(opts)

	return func(x *vigo.X) {
		p := x.PathParams.Get("path")
		if p == "" || p == "/" {
			p = "."
		} else if len(p) > 0 && p[0] == '/' {
			p = p[1:]
		}

		file, err := filesystem.Open(p)
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
			entries, err := readDir(filesystem, p)
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
			etag := getETag(p, stat, options.ETagCache)

			// Check for conditional requests (304)
			if checkNotModified(x.Request, etag, stat.ModTime()) {
				x.WriteHeader(http.StatusNotModified)
				return
			}

			setCacheHeaders(x.ResponseWriter(), etag, stat.ModTime(), options.CacheControl)
			http.ServeContent(x.ResponseWriter(), x.Request, stat.Name(), stat.ModTime(), rs)
			return
		}
		x.WriteHeader(http.StatusInternalServerError)
	}
}

// NewHandlerWithDefault returns a vigo handler with a default file fallback.
// If the requested path is not found or is a directory, the default file is served.
// The default file is read on-demand at runtime (not pre-loaded into memory).
//
// HTTP Cache Support:
//   - ETag: Generated from file size and modification time (or from ETagCache if provided)
//   - Last-Modified: File modification time
//   - 304 Not Modified: Returned when If-None-Match or If-Modified-Since match
//   - Cache-Control: Configurable via HandlerOptions
func NewHandlerWithDefault(filesystem fs.FS, defaultPath string, opts ...*HandlerOptions) func(*vigo.X) {
	options := mergeOptions(opts)

	// Check if default file exists and get its info
	var defaultExists bool
	var defaultModTime time.Time
	var defaultName string
	var defaultETag string

	if df, err := filesystem.Open(defaultPath); err == nil {
		if stat, err := df.Stat(); err == nil && !stat.IsDir() {
			defaultExists = true
			defaultModTime = stat.ModTime()
			defaultName = stat.Name()
			defaultETag = getETag(defaultPath, stat, options.ETagCache)
		}
		df.Close()
	}

	if !defaultExists {
		logv.Warn().Msgf("default file %s not found or is a directory", defaultPath)
	}

	return func(x *vigo.X) {
		p := x.PathParams.Get("path")
		if p == "" || p == "/" {
			p = "."
		} else if len(p) > 0 && p[0] == '/' {
			p = p[1:]
		}

		file, err := filesystem.Open(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && defaultExists {
				// If path has extension, return 404
				if path.Ext(p) != "" {
					x.WriteHeader(http.StatusNotFound)
					return
				}

				// Serve default file
				serveDefaultFile(x, filesystem, defaultPath, defaultName, defaultModTime, defaultETag, options)
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
			if defaultExists {
				// Serve default file for directory paths
				serveDefaultFile(x, filesystem, defaultPath, defaultName, defaultModTime, defaultETag, options)
				return
			}

			entries, err := readDir(filesystem, p)
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
			etag := getETag(p, stat, options.ETagCache)

			// Check for conditional requests (304)
			if checkNotModified(x.Request, etag, stat.ModTime()) {
				x.WriteHeader(http.StatusNotModified)
				return
			}

			setCacheHeaders(x.ResponseWriter(), etag, stat.ModTime(), options.CacheControl)
			http.ServeContent(x.ResponseWriter(), x.Request, stat.Name(), stat.ModTime(), rs)
			return
		}
		x.WriteHeader(http.StatusInternalServerError)
	}
}

// serveDefaultFile serves the default file with proper caching
func serveDefaultFile(x *vigo.X, filesystem fs.FS, defaultPath, defaultName string, defaultModTime time.Time, defaultETag string, options *HandlerOptions) {
	// Check for conditional requests (304)
	if checkNotModified(x.Request, defaultETag, defaultModTime) {
		x.WriteHeader(http.StatusNotModified)
		return
	}

	file, err := filesystem.Open(defaultPath)
	if err != nil {
		x.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if rs, ok := file.(io.ReadSeeker); ok {
		setCacheHeaders(x.ResponseWriter(), defaultETag, defaultModTime, options.CacheControl)
		http.ServeContent(x.ResponseWriter(), x.Request, defaultName, defaultModTime, rs)
		return
	}
	x.WriteHeader(http.StatusInternalServerError)
}

// readDir is a helper to read directory entries from any fs.FS
func readDir(filesystem fs.FS, name string) ([]fs.DirEntry, error) {
	if rdfs, ok := filesystem.(fs.ReadDirFS); ok {
		return rdfs.ReadDir(name)
	}
	// Fallback
	file, err := filesystem.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if rdf, ok := file.(fs.ReadDirFile); ok {
		return rdf.ReadDir(-1)
	}
	return nil, fmt.Errorf("file does not support ReadDir: %s", name)
}
