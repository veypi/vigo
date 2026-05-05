//
// httpfs.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/logv"
)

// fileEntry describes a file or directory in a JSON directory listing.
type fileEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Mime    string `json:"mime"`
	ModTime int64  `json:"mod_time"`
}

// defaultFileInfo holds pre-computed metadata for a default/fallback file.
type defaultFileInfo struct {
	path    string
	name    string
	modTime time.Time
	etag    string
}

// PathFunc extracts the raw file path from a request context.
// validatePath is applied afterwards — custom implementations don't need to validate.
// Return an error to short-circuit the request (e.g., 403 for unauthorized paths).
// Default: returns x.PathParams.Get("path").
type PathFunc func(*vigo.X) (string, error)

// defaultPathFunc extracts path from route params.
func defaultPathFunc(x *vigo.X) (string, error) {
	return x.PathParams.Get("path"), nil
}

// resolvePath extracts the raw path via PathFunc, then cleans and validates it.
func resolvePath(x *vigo.X, op string, pf PathFunc) (string, error) {
	if pf == nil {
		pf = defaultPathFunc
	}
	p, err := pf(x)
	if err != nil {
		return "", err
	}
	return validatePath(p, op)
}

// HandlerOptions configures the HTTP handler behavior for read operations.
type HandlerOptions struct {
	// CacheControl sets the Cache-Control header
	// Default: "public, max-age=0, must-revalidate"
	CacheControl string

	// ETagCache is a map of pre-computed ETags (path -> etag)
	// Used by EmbedFS to avoid runtime ETag calculation
	ETagCache map[string]string

	// PathFunc extracts the file path from the request context.
	// Default: x.PathParams.Get("path")
	PathFunc PathFunc
}

// RWOptions configures the HTTP handler behavior for read/write operations.
type RWOptions struct {
	HandlerOptions

	// AllowPut enables PUT method for file upload/overwrite
	AllowPut bool

	// AllowDelete enables DELETE method for file/directory removal
	AllowDelete bool

	// AllowMkdir enables MKCOL method for directory creation
	AllowMkdir bool

	// AllowRename enables PATCH method for rename/move
	AllowRename bool

	// MaxFileSize is the maximum upload size in bytes (0 = unlimited)
	MaxFileSize int64
}

// DefaultHandlerOptions returns default options for read-only handlers.
func DefaultHandlerOptions() *HandlerOptions {
	return &HandlerOptions{
		CacheControl: "public, max-age=0, must-revalidate",
		ETagCache:    nil,
	}
}

// DefaultRWOptions returns default options for read/write handlers.
func DefaultRWOptions() *RWOptions {
	return &RWOptions{
		HandlerOptions: *DefaultHandlerOptions(),
		AllowPut:       true,
		AllowDelete:    true,
		AllowMkdir:     true,
		AllowRename:    true,
		MaxFileSize:    0,
	}
}

// mergeOptions merges provided options with defaults.
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
		if opt.PathFunc != nil {
			result.PathFunc = opt.PathFunc
		}
	}
	return result
}

// mergeRWOptions merges provided RWOptions with defaults.
func mergeRWOptions(opts []*RWOptions) *RWOptions {
	result := DefaultRWOptions()
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
		if opt.PathFunc != nil {
			result.PathFunc = opt.PathFunc
		}
		result.AllowPut = opt.AllowPut
		result.AllowDelete = opt.AllowDelete
		result.AllowMkdir = opt.AllowMkdir
		result.AllowRename = opt.AllowRename
		if opt.MaxFileSize > 0 {
			result.MaxFileSize = opt.MaxFileSize
		} else if opt.MaxFileSize < 0 {
			result.MaxFileSize = 0
		}
	}
	return result
}

// getETag returns the ETag for a file, using cache if available.
func getETag(filePath string, info fs.FileInfo, cache map[string]string) string {
	if cache != nil {
		if etag, ok := cache[filePath]; ok {
			return etag
		}
	}
	return generateETag(info.Size(), info.ModTime())
}

// checkNotModified checks if the request has matching If-None-Match or If-Modified-Since headers.
// Returns true if the response should be 304 Not Modified.
func checkNotModified(r *http.Request, etag string, modTime time.Time) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return inm == etag || inm == "*"
	}

	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if imsTime, err := http.ParseTime(ims); err == nil {
			return !modTime.After(imsTime)
		}
	}

	return false
}

// setCacheHeaders sets common cache-related headers.
func setCacheHeaders(w http.ResponseWriter, etag string, modTime time.Time, cacheControl string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", cacheControl)
}

// =============================================================================
// Internal: GET / HEAD handlers
// =============================================================================

// handleGet serves a file or directory listing for GET requests.
func handleGet(x *vigo.X, filesystem fs.FS, options *HandlerOptions) {
	p, err := resolvePath(x, "open", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
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
		serveDirList(x, filesystem, p)
		return
	}

	if rs, ok := file.(io.ReadSeeker); ok {
		etag := getETag(p, stat, options.ETagCache)

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

// handleGetWithDefault serves GET requests with SPA-style fallback to a default file.
func handleGetWithDefault(x *vigo.X, filesystem fs.FS, options *HandlerOptions, df *defaultFileInfo) {
	p, err := resolvePath(x, "open", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	file, err := filesystem.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && df != nil {
			if path.Ext(p) != "" {
				x.WriteHeader(http.StatusNotFound)
				return
			}
			serveDefaultFile(x, filesystem, df, options)
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
		if df != nil {
			serveDefaultFile(x, filesystem, df, options)
			return
		}
		serveDirList(x, filesystem, p)
		return
	}

	if rs, ok := file.(io.ReadSeeker); ok {
		etag := getETag(p, stat, options.ETagCache)

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

// handleHead serves HEAD requests (headers only, no body).
func handleHead(x *vigo.X, filesystem fs.FS, options *HandlerOptions) {
	p, err := resolvePath(x, "open", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
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
		serveDirListHead(x, filesystem, p)
		return
	}

	if rs, ok := file.(io.ReadSeeker); ok {
		etag := getETag(p, stat, options.ETagCache)

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

// handleHeadWithDefault serves HEAD requests with SPA fallback.
func handleHeadWithDefault(x *vigo.X, filesystem fs.FS, options *HandlerOptions, df *defaultFileInfo) {
	p, err := resolvePath(x, "open", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	file, err := filesystem.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && df != nil {
			if path.Ext(p) != "" {
				x.WriteHeader(http.StatusNotFound)
				return
			}
			serveDefaultFileHead(x, filesystem, df, options)
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
		if df != nil {
			serveDefaultFileHead(x, filesystem, df, options)
			return
		}
		serveDirListHead(x, filesystem, p)
		return
	}

	if rs, ok := file.(io.ReadSeeker); ok {
		etag := getETag(p, stat, options.ETagCache)

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

// =============================================================================
// Internal: Write handlers
// =============================================================================

// handlePut creates or overwrites a file with the request body.
func handlePut(x *vigo.X, filesystem FS, options *RWOptions) {
	p, err := resolvePath(x, "write", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	// Check if path is an existing directory
	if statFS, ok := filesystem.(fs.StatFS); ok {
		if info, err := statFS.Stat(p); err == nil && info.IsDir() {
			x.WriteHeader(http.StatusConflict)
			return
		}
	}

	// Read request body
	var body []byte
	if options.MaxFileSize > 0 {
		x.Request.Body = http.MaxBytesReader(x.ResponseWriter(), x.Request.Body, options.MaxFileSize)
	}
	body, err = io.ReadAll(x.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			x.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := filesystem.WriteFile(p, body, 0o644); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			x.WriteHeader(http.StatusForbidden)
			return
		}
		x.WriteHeader(http.StatusInternalServerError)
		return
	}

	x.WriteHeader(http.StatusCreated)
}

// handleDelete removes a file or directory.
func handleDelete(x *vigo.X, filesystem FS, pf PathFunc) {
	p, err := resolvePath(x, "remove", pf)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := filesystem.RemoveAll(p); err != nil {
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

	x.WriteHeader(http.StatusNoContent)
}

// handleMkcol creates a directory.
func handleMkcol(x *vigo.X, filesystem FS, pf PathFunc) {
	p, err := resolvePath(x, "mkdir", pf)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	// Check if already exists
	if statFS, ok := filesystem.(fs.StatFS); ok {
		if _, err := statFS.Stat(p); err == nil {
			x.WriteHeader(http.StatusConflict)
			return
		}
	}

	if err := filesystem.MkdirAll(p, 0o755); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			x.WriteHeader(http.StatusForbidden)
			return
		}
		x.WriteHeader(http.StatusInternalServerError)
		return
	}

	x.WriteHeader(http.StatusCreated)
}

// patchRequest is the JSON body for PATCH requests.
type patchRequest struct {
	Action string `json:"action"`
	To     string `json:"to"`
}

// handlePatch handles rename/move requests.
func handlePatch(x *vigo.X, filesystem FS, pf PathFunc) {
	p, err := resolvePath(x, "rename", pf)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	var req patchRequest
	if err := json.NewDecoder(x.Request.Body).Decode(&req); err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	if req.Action != "rename" {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	if req.To == "" {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	// Normalize target path
	to := req.To
	if len(to) > 0 && to[0] == '/' {
		to = to[1:]
	}

	// Check source exists
	if statFS, ok := filesystem.(fs.StatFS); ok {
		if _, err := statFS.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				x.WriteHeader(http.StatusNotFound)
				return
			}
		}
		// Check target doesn't exist
		if _, err := statFS.Stat(to); err == nil {
			x.WriteHeader(http.StatusConflict)
			return
		}
	}

	if err := filesystem.Rename(p, to); err != nil {
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

	x.WriteHeader(http.StatusOK)
}

// handleOptions responds with the Allow header based on enabled operations.
func handleOptions(x *vigo.X, options *RWOptions) {
	methods := "GET, HEAD, OPTIONS"
	if options.AllowPut {
		methods += ", PUT"
	}
	if options.AllowDelete {
		methods += ", DELETE"
	}
	if options.AllowMkdir {
		methods += ", MKCOL"
	}
	if options.AllowRename {
		methods += ", PATCH"
	}
	x.Header().Set("Allow", methods)
	x.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Directory listing helpers
// =============================================================================

// buildDirList reads directory entries and returns a JSON-serializable list.
func buildDirList(filesystem fs.FS, p string) ([]fileEntry, error) {
	entries, err := readDir(filesystem, p)
	if err != nil {
		return nil, err
	}

	list := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		var size int64
		var modTime int64
		var mimeType string
		if err == nil {
			size = info.Size()
			modTime = info.ModTime().Unix()
			if !e.IsDir() {
				mimeType = mime.TypeByExtension(path.Ext(e.Name()))
			}
		}
		list = append(list, fileEntry{
			Name:    e.Name(),
			Dir:     e.IsDir(),
			Size:    size,
			ModTime: modTime,
			Mime:    mimeType,
		})
	}
	return list, nil
}

// serveDirList writes a JSON directory listing to the response.
func serveDirList(x *vigo.X, filesystem fs.FS, p string) {
	list, err := buildDirList(filesystem, p)
	if err != nil {
		x.WriteHeader(http.StatusInternalServerError)
		return
	}
	x.JSON(list)
}

// serveDirListHead sets headers for a directory listing without writing the body.
func serveDirListHead(x *vigo.X, filesystem fs.FS, p string) {
	list, err := buildDirList(filesystem, p)
	if err != nil {
		x.WriteHeader(http.StatusInternalServerError)
		return
	}
	data, _ := json.Marshal(list)
	x.Header().Set("Content-Type", "application/json")
	x.Header().Set("Content-Length", strconv.Itoa(len(data)))
	x.WriteHeader(http.StatusOK)
}

// =============================================================================
// Default file helpers
// =============================================================================

// resolveDefaultFileInfo reads the default file metadata at construction time.
func resolveDefaultFileInfo(filesystem fs.FS, defaultPath string, options *HandlerOptions) *defaultFileInfo {
	df, err := filesystem.Open(defaultPath)
	if err != nil {
		logv.Warn().Msgf("default file %s not found: %v", defaultPath, err)
		return nil
	}
	defer df.Close()

	stat, err := df.Stat()
	if err != nil || stat.IsDir() {
		logv.Warn().Msgf("default file %s is a directory or cannot be stat'ed", defaultPath)
		return nil
	}

	return &defaultFileInfo{
		path:    defaultPath,
		name:    stat.Name(),
		modTime: stat.ModTime(),
		etag:    getETag(defaultPath, stat, options.ETagCache),
	}
}

// serveDefaultFile serves the default file with caching headers.
func serveDefaultFile(x *vigo.X, filesystem fs.FS, df *defaultFileInfo, options *HandlerOptions) {
	if checkNotModified(x.Request, df.etag, df.modTime) {
		x.WriteHeader(http.StatusNotModified)
		return
	}

	file, err := filesystem.Open(df.path)
	if err != nil {
		x.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if rs, ok := file.(io.ReadSeeker); ok {
		setCacheHeaders(x.ResponseWriter(), df.etag, df.modTime, options.CacheControl)
		http.ServeContent(x.ResponseWriter(), x.Request, df.name, df.modTime, rs)
		return
	}
	x.WriteHeader(http.StatusInternalServerError)
}

// serveDefaultFileHead handles HEAD requests for the default file.
func serveDefaultFileHead(x *vigo.X, filesystem fs.FS, df *defaultFileInfo, options *HandlerOptions) {
	if checkNotModified(x.Request, df.etag, df.modTime) {
		x.WriteHeader(http.StatusNotModified)
		return
	}

	file, err := filesystem.Open(df.path)
	if err != nil {
		x.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if rs, ok := file.(io.ReadSeeker); ok {
		setCacheHeaders(x.ResponseWriter(), df.etag, df.modTime, options.CacheControl)
		http.ServeContent(x.ResponseWriter(), x.Request, df.name, df.modTime, rs)
		return
	}
	x.WriteHeader(http.StatusInternalServerError)
}

// =============================================================================
// Public API
// =============================================================================

// NewHandler returns a vigo handler that serves static files from the FS.
// It expects a path parameter named "path" in the route (e.g., "/static/{path:*}").
// If the path resolves to a directory, it returns a JSON list of the directory contents.
//
// HTTP Cache Support:
//   - ETag: Generated from file size and modification time (or from ETagCache if provided)
//   - Last-Modified: File modification time
//   - 304 Not Modified: Returned when If-None-Match or If-Modified-Since match
//   - Cache-Control: Configurable via HandlerOptions
func NewHandler(fsLoader *fs.FS, opts ...*HandlerOptions) func(*vigo.X) {
	options := mergeOptions(opts)
	return func(x *vigo.X) {
		handleGet(x, *fsLoader, options)
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
	df := resolveDefaultFileInfo(filesystem, defaultPath, options)

	return func(x *vigo.X) {
		handleGetWithDefault(x, filesystem, options, df)
	}
}

// NewRWHandler returns a vigo handler with full read/write support for ufs.FS.
// It expects a path parameter named "path" in the route (e.g., "/fs/{path:*}").
// Mount with router.Any() to capture all HTTP methods.
//
// Supported methods:
//   - GET: Read file or list directory
//   - HEAD: Same as GET but returns headers only
//   - PUT: Create/overwrite file (request body = file content)
//   - DELETE: Remove file or directory
//   - MKCOL: Create directory
//   - PATCH: Rename/move (JSON body: {"action": "rename", "to": "/new/path"})
//   - OPTIONS: Returns Allow header
//
// Write operations can be individually disabled via RWOptions.
func NewRWHandler(fsLoader *FS, opts ...*RWOptions) func(*vigo.X) {
	options := mergeRWOptions(opts)

	return func(x *vigo.X) {
		filesystem := *fsLoader
		switch x.Request.Method {
		case http.MethodGet:
			handleGet(x, filesystem, &options.HandlerOptions)
		case http.MethodHead:
			handleHead(x, filesystem, &options.HandlerOptions)
		case http.MethodPut:
			if !options.AllowPut {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handlePut(x, filesystem, options)
		case http.MethodDelete:
			if !options.AllowDelete {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handleDelete(x, filesystem, options.PathFunc)
		case "MKCOL":
			if !options.AllowMkdir {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handleMkcol(x, filesystem, options.PathFunc)
		case http.MethodPatch:
			if !options.AllowRename {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handlePatch(x, filesystem, options.PathFunc)
		case http.MethodOptions:
			handleOptions(x, options)
		default:
			x.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// NewRWHandlerWithDefault returns a vigo handler with full read/write support
// and SPA-style fallback to a default file for GET/HEAD requests.
// Write operations are not affected by the default file fallback.
func NewRWHandlerWithDefault(filesystem FS, defaultPath string, opts ...*RWOptions) func(*vigo.X) {
	options := mergeRWOptions(opts)
	df := resolveDefaultFileInfo(filesystem, defaultPath, &options.HandlerOptions)

	return func(x *vigo.X) {
		switch x.Request.Method {
		case http.MethodGet:
			handleGetWithDefault(x, filesystem, &options.HandlerOptions, df)
		case http.MethodHead:
			handleHeadWithDefault(x, filesystem, &options.HandlerOptions, df)
		case http.MethodPut:
			if !options.AllowPut {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handlePut(x, filesystem, options)
		case http.MethodDelete:
			if !options.AllowDelete {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handleDelete(x, filesystem, options.PathFunc)
		case "MKCOL":
			if !options.AllowMkdir {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handleMkcol(x, filesystem, options.PathFunc)
		case http.MethodPatch:
			if !options.AllowRename {
				x.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			handlePatch(x, filesystem, options.PathFunc)
		case http.MethodOptions:
			handleOptions(x, options)
		default:
			x.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// readDir is a helper to read directory entries from any fs.FS.
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
