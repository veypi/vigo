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
	"bytes"
	_ "embed"
	"io/fs"
	"strconv"
	"text/template"

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
// docFS is an fs.FS containing documentation files. prefix scopes docFS
// to a subdirectory (e.g., "docs" for //go:embed docs/*.md).
//
// The handler serves an embedded SPA for browser navigation and uses
// ufs for directory listing, file content, and search.
func New(router vigo.Router, docFS fs.FS, prefix string, opts ...*DocOptions) {
	if prefix != "" && prefix != "." {
		if sub, err := fs.Sub(docFS, prefix); err == nil {
			docFS = sub
		} else {
			logv.Warn().Msgf("doc: fs.Sub(%q) failed: %v, using root", prefix, err)
		}
	}

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

	vars := func() map[string]any {
		return map[string]any{
			"PREFIX": router.String(),
			"DEPTH":  strconv.Itoa(maxDepth),
		}
	}

	guide := execTemplate("guide.md", guideMD, vars())

	handler := ufs.NewHandler(&docFS,
		ufs.WithSpa("index.html", []byte(indexHTML), vars),
		ufs.WithMaxDepth(maxDepth),
		ufs.WithCacheControl(cacheControl),
		ufs.WithAllowSearch(true),
	)

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

func execTemplate(name, text string, data any) string {
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		return text
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return text
	}
	return buf.String()
}
