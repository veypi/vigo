package doc

import (
	"io/fs"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/contrib/common"
)

var ErrFailRead = &vigo.Error{Code: 500, Message: "failed to read file or directory: %s\n%s"}

func New(r vigo.Router, docFS fs.FS, prefix string) *DocFS {
	d := &DocFS{
		docFS:  docFS,
		prefix: prefix,
		router: r,
	}
	d.router.After(common.JsonResponse, common.JsonErrorResponse)
	d.router.Get("/", d.Dir)
	d.router.Get("/{path:*}", d.Dir)
	return d
}

type DocFS struct {
	docFS  fs.FS
	prefix string
	router vigo.Router
}

type ItemResponse struct {
	Name     string `json:"name"`
	Filename string `json:"filename" usage:"absolute path"`
	IsDir    bool   `json:"is_dir"`
}

type DirOpts struct {
	Path  string  `json:"path" src:"path" desc:"The path to the directory."`
	Depth int     `json:"depth" src:"query" desc:"The depth of the directory to list. -1 is unlimit" default:"1"`
	Toc   *bool   `json:"toc" src:"query" desc:"If true, return the table of contents of the markdown file."`
	From  *string `json:"from" src:"query" desc:"Start section number (inclusive)."`
	To    *string `json:"to" src:"query" desc:"End section number (inclusive)."`
}
