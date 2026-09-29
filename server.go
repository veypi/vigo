//
// server.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-08-06 20:00
// Distributed under terms of the MIT license.
//

package vigo

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"uuid"

	"github.com/veypi/vigo/logv"
	"golang.org/x/net/netutil"
)

func NewServer(opts ...func(*Config)) (*Application, error) {
	c := &Config{
		Host: "0.0.0.0",
		Port: 8000,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.listener == nil {
		// 注入 listener 场景（随机端口）：监听已真实建立，Host/Port 校验跳过
		if err := c.IsValid(); err != nil {
			return nil, err
		}
	}
	app := &Application{
		config: c,
		router: NewRouter(),
	}
	app.router.(*route).config = c
	app.server = &http.Server{
		Addr:              c.Url(),
		TLSConfig:         c.TlsCfg,
		ReadTimeout:       c.ReadTimeout,
		ReadHeaderTimeout: c.ReadHeaderTimeout,
		WriteTimeout:      c.WriteTimeout,
		IdleTimeout:       c.IdleTimeout,
		MaxHeaderBytes:    c.MaxHeaderBytes,
		TLSNextProto:      nil,
		ConnState:         nil,
		ErrorLog:          nil,
	}
	app.server.Handler = app

	return app, nil
}

type Application struct {
	router   Router
	muxs     []func(http.ResponseWriter, *http.Request) func(http.ResponseWriter, *http.Request)
	config   *Config
	server   *http.Server
	listener net.Listener
	shutdown sync.Once
}

func (app *Application) SetMux(m func(w http.ResponseWriter, r *http.Request) func(http.ResponseWriter, *http.Request)) {
	app.muxs = append(app.muxs, m)
}

func (app *Application) Domain(d string) Router {
	newNouter := NewRouter()
	fc := func(w http.ResponseWriter, r *http.Request) func(http.ResponseWriter, *http.Request) {
		if r.Host == d {
			return newNouter.ServeHTTP
		}
		return nil
	}
	if strings.HasPrefix(d, "*.") {
		d = strings.Replace(d, "*.", "", 1)
		fc = func(w http.ResponseWriter, r *http.Request) func(http.ResponseWriter, *http.Request) {
			if strings.HasSuffix(r.Host, d) {
				return newNouter.ServeHTTP
			}
			return nil
		}
	}
	app.muxs = append(app.muxs, fc)
	return newNouter
}

func (app *Application) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := app.requestID(r)
	w.Header().Set(app.config.RequestIDHeader, reqID)
	ctx := context.WithValue(r.Context(), requestIDContextKey, reqID)
	r = r.WithContext(ctx)
	rw := &responseCapture{ResponseWriter: w}
	if !app.config.DisableReqLog {
		start := nanotime()
		defer func() {
			if rw.StatusCode() >= 500 {
				logv.WithNoCaller.Warn().
					Str("request_id", reqID).
					Int("status", rw.StatusCode()).
					Int64("ms", (nanotime()-start)/1e6).
					Str("method", r.Method).
					Msg(r.RequestURI)
			} else {
				logv.WithNoCaller.Debug().
					Str("request_id", reqID).
					Int("status", rw.StatusCode()).
					Int64("ms", (nanotime()-start)/1e6).
					Str("method", r.Method).
					Msg(r.RequestURI)
			}
		}()
	}
	if len(app.muxs) == 0 {
		app.router.ServeHTTP(rw, r)
		return
	}
	for _, fc := range app.muxs {
		if tmp := fc(rw, r); tmp != nil {
			tmp(rw, r)
			return
		}
	}
	app.router.ServeHTTP(rw, r)
}

func (app *Application) Router() Router {
	return app.router
}

func (app *Application) SetRouter(r Router) {
	app.router = r
}

func (app *Application) Run() error {
	l, e := app.netListener()
	if e != nil {
		return e
	}
	logv.WithNoCaller.Info().Msgf("start on %s", app.Addr())
	err := app.server.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Addr 返回实际监听地址：注入 listener 时以其为准（随机端口场景），
// 否则返回 {host}:{port}（0.0.0.0 展示为 localhost）。
func (app *Application) Addr() string {
	l := app.listener
	if l == nil {
		l = app.config.listener
	}
	if l != nil {
		return l.Addr().String()
	}
	host := app.config.Host
	if host == "0.0.0.0" {
		host = "localhost"
	}
	return fmt.Sprintf("%s:%d", host, app.config.Port)
}

func (app *Application) netListener() (net.Listener, error) {
	if app.listener != nil {
		return app.listener, nil
	}
	// 注入的 listener（WithListener）：首次使用时接管
	if app.config.listener != nil {
		app.listener = app.config.listener
		return app.listener, nil
	}
	l, err := net.Listen("tcp", app.config.Url())
	if err != nil {
		return nil, err
	}
	if app.config.TlsCfg != nil && len(app.config.TlsCfg.Certificates) > 0 && app.config.TlsCfg.GetCertificate != nil {
		l = tls.NewListener(l, app.config.TlsCfg)
	}
	if app.config.MaxConnections > 0 {
		l = netutil.LimitListener(l, app.config.MaxConnections)
	}
	app.listener = l
	return app.listener, nil
}

func (app *Application) Shutdown(ctx context.Context) error {
	var err error
	app.shutdown.Do(func() {
		err = app.server.Shutdown(ctx)
	})
	return err
}

func (app *Application) Close() error {
	var err error
	app.shutdown.Do(func() {
		err = app.server.Close()
	})
	return err
}

func (app *Application) requestID(r *http.Request) string {
	header := app.config.RequestIDHeader
	if header == "" {
		header = "X-Request-ID"
	}
	if reqID := strings.TrimSpace(r.Header.Get(header)); reqID != "" {
		return reqID
	}
	return uuid.New().String()
}

type responseCapture struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseCapture) WriteHeader(statusCode int) {
	if r.statusCode == 0 {
		r.statusCode = statusCode
	}
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseCapture) Write(p []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	return r.ResponseWriter.Write(p)
}

func (r *responseCapture) StatusCode() int {
	if r.statusCode == 0 {
		return http.StatusOK
	}
	return r.statusCode
}

func (r *responseCapture) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		if r.statusCode == 0 {
			r.statusCode = http.StatusOK
		}
		flusher.Flush()
	}
}

func (r *responseCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (r *responseCapture) Push(target string, opts *http.PushOptions) error {
	pusher, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (r *responseCapture) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		if r.statusCode == 0 {
			r.statusCode = http.StatusOK
		}
		return rf.ReadFrom(src)
	}
	return io.Copy(r.ResponseWriter, src)
}
