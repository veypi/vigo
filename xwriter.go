//
// xwriter.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package vigo

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"unsafe"
)

func (x *X) Header() http.Header {
	return x.writer.Header()
}

func (x *X) WriteHeader(statusCode int) {
	if x.wroteHeader {
		return
	}
	x.wroteHeader = true
	x.statusCode = statusCode
	x.writer.WriteHeader(statusCode)
}

func (x *X) Write(p []byte) (n int, err error) {
	if !x.wroteHeader {
		x.WriteHeader(http.StatusOK)
	}
	return x.writer.Write(p)
}

func (x *X) WriteString(s string) (n int, err error) {
	return x.Write(unsafe.Slice(unsafe.StringData(s), len(s)))
}

func (x *X) String(code int, format string, values ...any) error {
	if x.Header().Get("Content-Type") == "" {
		x.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	x.WriteHeader(code)
	_, err := x.WriteString(fmt.Sprintf(format, values...))
	return err
}

func (x *X) JSON(data any) error {
	var err error
	switch v := data.(type) {
	case string:
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, err = x.Write([]byte(v))
	case []byte:
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "application/octet-stream")
		}
		_, err = x.Write(v)
	case error:
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, err = x.Write([]byte(v.Error()))
	case nil:
	case int, uint, int8, uint8, int16, uint16, int32, uint32, int64, uint64, float32, float64, bool:
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		_, err = x.Write(fmt.Appendf([]byte{}, "%v", v))
	default:
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if x.Header().Get("Content-Type") == "" {
			x.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		_, err = x.Write(b)
	}
	return err
}

func (x *X) HTMLTemplate(tpl string, data any) error {
	x.Header().Set("Content-Type", "text/html; charset=utf-8")
	t, err := template.New("").Parse(tpl)
	if err != nil {
		return err
	}
	return t.Execute(x.writer, data)
}

func (x *X) Embed(fs *embed.FS, fpath string) error {
	file, err := fs.Open(fpath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	// http.ServeContent handles Content-Type, Content-Length, Range requests, and Last-Modified
	http.ServeContent(x.writer, x.Request, filepath.Base(fpath), info.ModTime(), file.(io.ReadSeeker))
	return nil
}

func (x *X) File(path string) error {
	// 打开文件
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}

	// http.ServeContent handles Content-Type, Content-Length, Range requests, and Last-Modified
	http.ServeContent(x.writer, x.Request, filepath.Base(path), fileInfo.ModTime(), file)
	return nil
}

func (x *X) Flush() {
	flusher, ok := x.writer.(http.Flusher)
	if !ok {
		http.Error(x.writer, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}
	flusher.Flush()
}
