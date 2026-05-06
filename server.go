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
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
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
	if err := c.IsValid(); err != nil {
		return nil, err
	}
	app := &Application{
		config: c,
		router: NewRouter(),
	}
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
			logv.Warn().Msg(r.Host)
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
	ctx := context.WithValue(r.Context(), configContextKey, app.config)
	ctx = context.WithValue(ctx, requestIDContextKey, reqID)
	r = r.WithContext(ctx)
	rw := &responseCapture{ResponseWriter: w}
	if !app.config.DisableReqLog {
		start := nanotime()
		defer func() {
			if rw.StatusCode() >= 400 {
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
	host := app.config.Host
	if host == "0.0.0.0" {
		host = "localhost"
	}
	logv.WithNoCaller.Info().Msgf("start on http://%s:%d ", host, app.config.Port)
	err := app.server.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (app *Application) netListener() (net.Listener, error) {
	if app.listener != nil {
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
	return uuid.NewString()
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
