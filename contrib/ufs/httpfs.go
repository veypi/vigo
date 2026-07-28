//
// httpfs.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/logv"
)

// =============================================================================
// Types
// =============================================================================

// spaFile holds pre-computed SPA metadata and content.
// If attrsFn is non-nil, content is the raw (untemplated) text and
// the template is resolved lazily on the first SPA request.
type spaFile struct {
	name    string
	content []byte
	etag    string
	modTime time.Time

	once    sync.Once
	attrsFn func() map[string]any // nil after lazy resolution
}

// spaConfig is the raw configuration before SPA resolution.
type spaConfig struct {
	name    string
	content []byte                // nil = read from file at resolve time
	attrsFn func() map[string]any // lazy template vars (nil = no templating)
}

// PathFunc extracts the raw file path from a request context.
// validatePath is applied afterwards — custom implementations don't need to validate.
// Return an error to short-circuit the request (e.g., 403 for unauthorized paths).
// Default: returns x.PathParams.Get("path").
type PathFunc func(*vigo.X) (string, error)

func defaultPathFunc(x *vigo.X) (string, error) {
	return x.PathParams.Get("path"), nil
}

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

// HandlerOptions configures the HTTP handler behavior for both read-only and read/write handlers.
// Write-specific fields (AllowPut, etc.) are ignored by NewHandler.
type HandlerOptions struct {
	CacheControl string
	ETagCache    map[string]string
	PathFunc     PathFunc
	MaxDepth     int
	AllowSearch  bool

	AllowPut    bool
	AllowDelete bool
	AllowMkdir  bool
	AllowRename bool
	MaxFileSize int64

	spaCfg  *spaConfig
	spa     *spaFile
	spaOnce sync.Once
}

// HttpOption is a functional option for HandlerOptions.
type HttpOption func(*HandlerOptions)

// =============================================================================
// Option functions
// =============================================================================

// WithSpa enables SPA fallback. When a browser (Accept: text/html) requests a
// non-existent path or directory, the SPA file is served instead.
//
//	content == nil → name is treated as a file path, read from the FS at construction.
//	content != nil → content is used directly as the SPA body.
//	resolver != nil → the content is executed as a Go template with the resolver's
//	                   return value as the data context. The resolver is called once,
//	                   lazily on the first SPA request — use this when template values
//	                   are not known at construction time (e.g., router prefix).
func WithSpa(name string, content []byte, resolver ...func() map[string]any) HttpOption {
	var fn func() map[string]any
	if len(resolver) > 0 {
		fn = resolver[0]
	}
	return func(o *HandlerOptions) {
		o.spaCfg = &spaConfig{
			name:    name,
			content: content,
			attrsFn: fn,
		}
	}
}

func WithCacheControl(v string) HttpOption {
	return func(o *HandlerOptions) { o.CacheControl = v }
}

func WithMaxDepth(d int) HttpOption {
	return func(o *HandlerOptions) { o.MaxDepth = d }
}

func WithAllowSearch(v bool) HttpOption {
	return func(o *HandlerOptions) { o.AllowSearch = v }
}

func WithPathFunc(pf PathFunc) HttpOption {
	return func(o *HandlerOptions) { o.PathFunc = pf }
}

func WithETagCache(m map[string]string) HttpOption {
	return func(o *HandlerOptions) { o.ETagCache = m }
}

func WithPut(v bool) HttpOption {
	return func(o *HandlerOptions) { o.AllowPut = v }
}

func WithDelete(v bool) HttpOption {
	return func(o *HandlerOptions) { o.AllowDelete = v }
}

func WithMkdir(v bool) HttpOption {
	return func(o *HandlerOptions) { o.AllowMkdir = v }
}

func WithRename(v bool) HttpOption {
	return func(o *HandlerOptions) { o.AllowRename = v }
}

func WithMaxFileSize(n int64) HttpOption {
	return func(o *HandlerOptions) { o.MaxFileSize = n }
}

// =============================================================================
// SPA resolution (construction time)
// =============================================================================

func resolveSpa(fsys fs.FS, cfg *spaConfig) (*spaFile, error) {
	var (
		content []byte
		modTime time.Time
	)

	if cfg.content == nil {
		f, err := fsys.Open(cfg.name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			return nil, err
		}
		modTime = stat.ModTime()
		content, err = io.ReadAll(f)
		if err != nil {
			return nil, err
		}
	} else {
		content = cfg.content
		modTime = time.Now()
	}

	spa := &spaFile{
		name:    cfg.name,
		content: content,
		etag:    generateETag(int64(len(content)), modTime),
		modTime: modTime,
		attrsFn: cfg.attrsFn,
	}
	return spa, nil
}

// =============================================================================
// Cache helpers
// =============================================================================

func getETag(filePath string, info fs.FileInfo, cache map[string]string) string {
	if cache != nil {
		if etag, ok := cache[filePath]; ok {
			return etag
		}
	}
	return generateETag(info.Size(), info.ModTime())
}

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

func setCacheHeaders(w http.ResponseWriter, etag string, modTime time.Time, cacheControl string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", modTime.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", cacheControl)
}

func skipFallback(x *vigo.X) bool {
	if x.Request.Header.Get("X-No-Fallback") != "" {
		return true
	}
	if x.Request.URL.Query().Has("x-no-fallback") {
		return true
	}
	accept := x.Request.Header.Get("Accept")
	if accept == "" {
		return true
	}
	return !strings.Contains(accept, "text/html")
}

// =============================================================================
// Search helpers
// =============================================================================

func parseSearchParams(r *http.Request) (glob, pattern string, limit int, ignoreCase, ok bool) {
	q := r.URL.Query()
	glob = q.Get("glob")
	if glob == "" {
		return "", "", 0, false, false
	}
	pattern = q.Get("pattern")
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	ignoreCase = q.Get("ignore_case") == "true"
	return glob, pattern, limit, ignoreCase, true
}

func trySearch(x *vigo.X, filesystem fs.FS, searchPath string, options *HandlerOptions) bool {
	glob, pattern, limit, ignoreCase, ok := parseSearchParams(x.Request)
	if !ok {
		return false
	}
	if !options.AllowSearch {
		x.WriteHeader(http.StatusForbidden)
		return true
	}
	results, err := Search(filesystem, searchPath, glob, pattern, limit, ignoreCase)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return true
	}
	x.JSON(results)
	return true
}

// =============================================================================
// SPA serving
// =============================================================================

// resolveSpaLazy executes the deferred SPA template resolution on first call.
func resolveSpaLazy(spa *spaFile) {
	spa.once.Do(func() {
		if spa.attrsFn == nil {
			return
		}
		vars := spa.attrsFn()
		tmpl, err := template.New(spa.name).Parse(string(spa.content))
		if err != nil {
			logv.Error().Msgf("ufs: SPA template parse %q: %v", spa.name, err)
			return
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, vars); err != nil {
			logv.Error().Msgf("ufs: SPA template execute %q: %v", spa.name, err)
			return
		}
		spa.content = buf.Bytes()
		spa.etag = generateETag(int64(len(spa.content)), time.Now())
		spa.attrsFn = nil
	})
}

func serveSpa(x *vigo.X, spa *spaFile, options *HandlerOptions) {
	resolveSpaLazy(spa)

	if checkNotModified(x.Request, spa.etag, spa.modTime) {
		x.WriteHeader(http.StatusNotModified)
		return
	}
	x.Header().Set("Content-Type", "text/html; charset=utf-8")
	setCacheHeaders(x.ResponseWriter(), spa.etag, spa.modTime, options.CacheControl)
	x.WriteHeader(http.StatusOK)
	x.Write(spa.content)
}

func serveSpaHead(x *vigo.X, spa *spaFile, options *HandlerOptions) {
	resolveSpaLazy(spa)

	if checkNotModified(x.Request, spa.etag, spa.modTime) {
		x.WriteHeader(http.StatusNotModified)
		return
	}
	x.Header().Set("Content-Type", "text/html; charset=utf-8")
	x.Header().Set("Content-Length", strconv.Itoa(len(spa.content)))
	setCacheHeaders(x.ResponseWriter(), spa.etag, spa.modTime, options.CacheControl)
	x.WriteHeader(http.StatusOK)
}

// =============================================================================
// GET / HEAD handlers
// =============================================================================

func handleGet(x *vigo.X, filesystem fs.FS, options *HandlerOptions) {
	p, err := resolvePath(x, "open", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	if trySearch(x, filesystem, p, options) {
		return
	}

	file, err := filesystem.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && options.spa != nil && !skipFallback(x) {
			serveSpa(x, options.spa, options)
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
		if options.spa != nil && !skipFallback(x) {
			serveSpa(x, options.spa, options)
			return
		}
		if options.MaxDepth <= 0 {
			if options.spa != nil {
				serveSpa(x, options.spa, options)
			} else {
				x.WriteHeader(http.StatusForbidden)
			}
			return
		}
		serveDirList(x, filesystem, p, stat, parseDepth(x.Request, options))
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

func handleHead(x *vigo.X, filesystem fs.FS, options *HandlerOptions) {
	p, err := resolvePath(x, "open", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	if _, _, _, _, ok := parseSearchParams(x.Request); ok {
		if !options.AllowSearch {
			x.WriteHeader(http.StatusForbidden)
			return
		}
		x.Header().Set("Content-Type", "application/json")
		x.WriteHeader(http.StatusOK)
		return
	}

	file, err := filesystem.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && options.spa != nil && !skipFallback(x) {
			serveSpaHead(x, options.spa, options)
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
		if options.spa != nil && !skipFallback(x) {
			serveSpaHead(x, options.spa, options)
			return
		}
		if options.MaxDepth <= 0 {
			if options.spa != nil {
				serveSpaHead(x, options.spa, options)
			} else {
				x.WriteHeader(http.StatusForbidden)
			}
			return
		}
		serveDirListHead(x, filesystem, p, stat, parseDepth(x.Request, options))
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
// Write handlers
// =============================================================================

func handlePut(x *vigo.X, filesystem FS, options *HandlerOptions) {
	p, err := resolvePath(x, "write", options.PathFunc)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

	if statFS, ok := filesystem.(fs.StatFS); ok {
		if info, err := statFS.Stat(p); err == nil && info.IsDir() {
			x.WriteHeader(http.StatusConflict)
			return
		}
	}

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

func handleMkcol(x *vigo.X, filesystem FS, pf PathFunc) {
	p, err := resolvePath(x, "mkdir", pf)
	if err != nil {
		x.WriteHeader(http.StatusBadRequest)
		return
	}

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

type patchRequest struct {
	Action string `json:"action"`
	To     string `json:"to"`
}

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

	to := req.To
	if len(to) > 0 && to[0] == '/' {
		to = to[1:]
	}

	if statFS, ok := filesystem.(fs.StatFS); ok {
		if _, err := statFS.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				x.WriteHeader(http.StatusNotFound)
				return
			}
		}
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

func handleOptions(x *vigo.X, options *HandlerOptions) {
	methods := "GET, HEAD, OPTIONS"
	if options.AllowPut {
		methods += ", PUT"
	}
	if options.AllowDelete {
		methods += ", DELETE"
	}
	if options.AllowMkdir {
		methods += ", POST"
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

func isGitRepo(filesystem fs.FS, p string) bool {
	if statFS, ok := filesystem.(fs.StatFS); ok {
		info, err := statFS.Stat(path.Join(p, ".git"))
		return err == nil && info.IsDir()
	}
	f, err := filesystem.Open(path.Join(p, ".git"))
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	return err == nil && info.IsDir()
}

func parseDepth(r *http.Request, options *HandlerOptions) int {
	if options.MaxDepth <= 0 {
		return 0
	}
	depth := 1
	if d, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil {
		depth = d
	}
	if depth < 1 {
		depth = 1
	}
	if depth > options.MaxDepth {
		depth = options.MaxDepth
	}
	return depth
}

func buildItemTree(filesystem fs.FS, p string, stat fs.FileInfo, depth int) (*ItemEntry, error) {
	entries, err := readDir(filesystem, p)
	if err != nil {
		return nil, err
	}

	items := make([]ItemEntry, 0, len(entries))
	for _, e := range entries {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			continue
		}
		info, err := e.Info()
		var size int64
		var modTime int64
		var mimeType string
		var isRepo bool
		if err == nil {
			size = info.Size()
			modTime = info.ModTime().Unix()
			if e.IsDir() {
				isRepo = isGitRepo(filesystem, path.Join(p, e.Name()))
			} else {
				mimeType = mime.TypeByExtension(path.Ext(e.Name()))
			}
		}

		item := ItemEntry{
			Name:    e.Name(),
			Dir:     e.IsDir(),
			Size:    size,
			ModTime: modTime,
			Mime:    mimeType,
			IsRepo:  isRepo,
		}

		if e.IsDir() && depth > 1 {
			subPath := path.Join(p, e.Name())
			subFile, err := filesystem.Open(subPath)
			if err == nil {
				subStat, sErr := subFile.Stat()
				subFile.Close()
				if sErr == nil {
					if subEntry, sErr := buildItemTree(filesystem, subPath, subStat, depth-1); sErr == nil {
						item.Items = subEntry.Items
					}
				}
			}
		}

		items = append(items, item)
	}

	return &ItemEntry{
		Name:    stat.Name(),
		Dir:     true,
		Size:    stat.Size(),
		ModTime: stat.ModTime().Unix(),
		IsRepo:  isGitRepo(filesystem, p),
		Items:   items,
	}, nil
}

func serveDirList(x *vigo.X, filesystem fs.FS, p string, stat fs.FileInfo, depth int) {
	entry, err := buildItemTree(filesystem, p, stat, depth)
	if err != nil {
		x.WriteHeader(http.StatusInternalServerError)
		return
	}
	x.JSON(entry)
}

func serveDirListHead(x *vigo.X, filesystem fs.FS, p string, stat fs.FileInfo, depth int) {
	entry, err := buildItemTree(filesystem, p, stat, depth)
	if err != nil {
		x.WriteHeader(http.StatusInternalServerError)
		return
	}
	data, _ := json.Marshal(entry)
	x.Header().Set("Content-Type", "application/json")
	x.Header().Set("Content-Length", strconv.Itoa(len(data)))
	x.WriteHeader(http.StatusOK)
}

// =============================================================================
// Public API
// =============================================================================

// NewHandler returns a vigo handler that serves static files from the FS.
//
//	handler := ufs.NewHandler(&fs,
//	    ufs.WithSpa("index.html", nil),
//	    ufs.WithMaxDepth(3),
//	    ufs.WithAllowSearch(true),
//	)
func NewHandler(fsLoader *fs.FS, opts ...HttpOption) func(*vigo.X) {
	options := &HandlerOptions{
		CacheControl: "public, max-age=0, must-revalidate",
	}
	for _, opt := range opts {
		opt(options)
	}

	return func(x *vigo.X) {
		filesystem := *fsLoader
		options.spaOnce.Do(func() {
			if options.spaCfg != nil {
				if spa, err := resolveSpa(filesystem, options.spaCfg); err != nil {
					logv.Warn().Msgf("ufs: SPA %q resolve failed: %v", options.spaCfg.name, err)
				} else {
					options.spa = spa
				}
			}
		})
		handleGet(x, filesystem, options)
	}
}

// NewRWHandler returns a vigo handler with full read/write support for ufs.FS.
// All write operations are enabled by default. Mount with router.Any().
//
//	handler := ufs.NewRWHandler(&fs,
//	    ufs.WithPathFunc(myPathFunc),
//	    ufs.WithMaxFileSize(10 << 20),
//	)
func NewRWHandler(fsLoader *FS, opts ...HttpOption) func(*vigo.X) {
	options := &HandlerOptions{
		CacheControl: "public, max-age=0, must-revalidate",
		AllowPut:     true,
		AllowDelete:  true,
		AllowMkdir:   true,
		AllowRename:  true,
	}
	for _, opt := range opts {
		opt(options)
	}

	return func(x *vigo.X) {
		filesystem := *fsLoader
		options.spaOnce.Do(func() {
			if options.spaCfg != nil {
				if spa, err := resolveSpa(filesystem, options.spaCfg); err != nil {
					logv.Warn().Msgf("ufs: SPA %q resolve failed: %v", options.spaCfg.name, err)
				} else {
					options.spa = spa
				}
			}
		})
		switch x.Request.Method {
		case http.MethodGet:
			handleGet(x, filesystem, options)
		case http.MethodHead:
			handleHead(x, filesystem, options)
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
		case http.MethodPost:
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

// =============================================================================
// Internal helpers
// =============================================================================

func readDir(filesystem fs.FS, name string) ([]fs.DirEntry, error) {
	if rdfs, ok := filesystem.(fs.ReadDirFS); ok {
		return rdfs.ReadDir(name)
	}
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
