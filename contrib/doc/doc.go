//
// doc.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

// Package doc provides a documentation browser middleware for vigo.
// It serves an embedded SPA for browsing markdown files via the ufs module.
package doc

import (
	_ "embed"
	"io/fs"
	"strconv"
	"strings"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/contrib/ufs"
	"github.com/veypi/vigo/logv"
)

//go:embed index.html
var indexHTML string

//go:embed guide.md
var guideMD string

//go:embed vhtml.html
var vhtmlHTML string

// DocOptions configures the doc handler behavior.
type DocOptions struct {
	// MaxDepth controls the maximum directory listing depth (1-99).
	// Default: 5
	MaxDepth int

	// CacheControl sets the Cache-Control header.
	// Default: "public, max-age=0"
	CacheControl string
}

// New mounts a documentation browser on the given router.
//
// docFS is an fs.FS containing markdown (.md) files. Non-.md files are
// filtered out automatically. prefix scopes docFS to a subdirectory
// (e.g., "docs" for //go:embed docs/*.md).
//
// The handler serves an index.html SPA for browser navigation and uses
// ufs for directory listing, file content, and search.
func New(router vigo.Router, docFS fs.FS, prefix string, opts ...*DocOptions) {
	// Apply prefix via fs.Sub
	if prefix != "" && prefix != "." {
		if sub, err := fs.Sub(docFS, prefix); err == nil {
			docFS = sub
		} else {
			logv.Warn().Msgf("doc: fs.Sub(%q) failed: %v, using root", prefix, err)
		}
	}

	// Merge options
	maxDepth := 5
	cacheControl := "public, max-age=0"
	if len(opts) > 0 && opts[0] != nil {
		if opts[0].MaxDepth > 0 {
			maxDepth = opts[0].MaxDepth
		}
		if opts[0].CacheControl != "" {
			cacheControl = opts[0].CacheControl
		}
	}

	// Inject router prefix and max depth into index.html
	html := strings.ReplaceAll(indexHTML, "{{PREFIX}}", router.String())
	html = strings.ReplaceAll(html, "{{DEPTH}}", strconv.Itoa(maxDepth))

	// Inject router prefix into guide
	guide := strings.ReplaceAll(guideMD, "{{PREFIX}}", router.String())

	// Create filtered FS that only exposes .md files + index.html
	dfs := newDocFS(docFS, html)
	var fsys fs.FS = dfs

	handler := ufs.NewHandlerWithDefault(&fsys, "index.html", &ufs.HandlerOptions{
		MaxDepth:     maxDepth,
		CacheControl: cacheControl,
		AllowSearch:  true,
	})

	router.Get("/", "文档首页", func(x *vigo.X) {
		// API requests (X-No-Fallback, depth, glob) go through the ufs handler
		r := x.Request
		if r.Header.Get("X-No-Fallback") != "" ||
			r.URL.Query().Has("depth") ||
			r.URL.Query().Has("glob") {
			handler(x)
			return
		}
		x.Header().Set("Content-Type", "text/html; charset=utf-8")
		x.WriteHeader(200)
		x.WriteString(html + "\n")
	})

	router.Get("/.guide", "使用指南", func(x *vigo.X) {
		x.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		x.WriteHeader(200)
		x.WriteString(guide + "\n")
	})

	router.Get("/vhtml.html", "VHTML组件文档", func(x *vigo.X) {
		x.Header().Set("Content-Type", "text/html; charset=utf-8")
		x.WriteHeader(200)
		x.WriteString(vhtmlHTML + "\n")
	})

	router.Get("/{path:*}", "文档浏览", handler)
	router.Head("/{path:*}", "文档信息", handler)
}
