//
// server.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-08-06 20:00
// Distributed under terms of the MIT license.
//

package vigo

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"

	"github.com/veypi/vigo/logv"
	"golang.org/x/net/netutil"
)

func New(opts ...func(*Config)) (*Application, error) {
	c := &Config{
		Host:    "0.0.0.0",
		Port:    8000,
		DocPath: "_api",
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
		ReadTimeout:       0,
		ReadHeaderTimeout: 0,
		WriteTimeout:      0,
		IdleTimeout:       0,
		MaxHeaderBytes:    0,
		TLSNextProto:      nil,
		ConnState:         nil,
		ErrorLog:          nil,
		// TODO
		BaseContext: nil,
		// TODO
		ConnContext: nil,
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
	if !app.config.DisableReqLog {
		start := nanotime()
		defer func() {
			logv.WithNoCaller.Info().Int64("ns", (nanotime()-start)/1e3).Str("method", r.Method).Msg(r.RequestURI)
		}()
	}
	if len(app.muxs) == 0 {
		app.router.ServeHTTP(w, r)
		return
	}
	for _, fc := range app.muxs {
		if tmp := fc(w, r); tmp != nil {
			tmp(w, r)
			return
		}
	}
	app.router.ServeHTTP(w, r)
}

func (app *Application) Router() Router {
	return app.router
}

func (app *Application) SetRouter(r Router) {
	app.router = r
}

func (app *Application) Run() error {
	app.EnableApiDoc()
	l, e := app.netListener()
	if e != nil {
		return e
	}
	host := app.config.Host
	if host == "0.0.0.0" {
		host = "localhost"
	}
	logv.WithNoCaller.Info().Msgf("start on http://%s:%d ", host, app.config.Port)
	return app.server.Serve(l)
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
