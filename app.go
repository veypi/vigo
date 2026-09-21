//
// app.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package vigo

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/veypi/vigo/contrib/event"
	"github.com/veypi/vigo/flags"
	"github.com/veypi/vigo/logv"
)

type App[T any] interface {
	// 自动路由集成(api,ui, static file etc.)
	Router() Router
	Name() string
	// 自动参数解析
	Config() T
	// 初始化函数
	Init() error
	// 退出函数
	Exit()
	Run() error
}

type appOptions struct {
	init func() error
	exit func()
}

func WithInit(fn func() error) func(*appOptions) {
	return func(o *appOptions) {
		o.init = fn
	}
}

func WithExit(fn func()) func(*appOptions) {
	return func(o *appOptions) {
		o.exit = fn
	}
}

func New[T any](name string, router Router, config T, opts ...func(*appOptions)) App[T] {
	o := &appOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return &app[T]{
		router: router,
		name:   name,
		cfg:    config,
		init:   o.init,
		exit:   o.exit,
	}
}

type app[T any] struct {
	router Router
	name   string
	cfg    T
	init   func() error
	exit   func()
}

func (a *app[T]) Router() Router {
	return a.router
}

func (a *app[T]) Name() string {
	return a.name
}

func (a *app[T]) Config() T {
	return a.cfg
}

func (a *app[T]) Init() error {
	if a.init != nil {
		return a.init()
	}
	return nil
}

func (a *app[T]) Exit() {
	if a.exit != nil {
		a.exit()
	}
}

// runOptions vigo 运行时选项，经 flags.AutoRegister 注册：
// flag 名来自 json tag（port 同时暴露 -p），env 为 tag 大写（HOST/PORT/...）。
type runOptions struct {
	Host        string `json:"host" default:"0.0.0.0" desc:"host address"`
	Port        int    `json:"port" short:"p" default:"4000" desc:"listen port"`
	LoggerLevel string `json:"logger_level" short:"l" default:"debug" desc:"logger level"`
	LoggerPath  string `json:"logger_path" desc:"logger file path"`
	LoggerMode  string `json:"logger_mode" default:"console" desc:"logger mode: console | nocolor | json"`
}

func (a *app[T]) Run() error {
	godotenv.Load()
	opts := &runOptions{}
	cmdMain := flags.New(a.Name(), "")
	cmdMain.AutoRegister(opts, a.Config())
	configFile := cmdMain.ConfigFileFlag("f", "./dev.yaml")
	cmdCfg := cmdMain.SubCommand("gen", "generate cfg file")
	cmdCfg.Command = func() error {
		return flags.DumpCfg(*configFile, a.Config())
	}
	cmdMain.Before = func() error {
		logv.SetLevel(logv.AssertFuncErr(logv.ParseLevel(opts.LoggerLevel)))
		var writers []io.Writer
		switch opts.LoggerMode {
		case "nocolor":
			writers = append(writers, logv.ConsoleWriterNoColor())
		case "json":
			writers = append(writers, os.Stdout)
		default:
			writers = append(writers, logv.ConsoleWriter())
		}
		if opts.LoggerPath != "" {
			logv.FileHook.Filename = opts.LoggerPath
			writers = append(writers, &logv.FileHook)
		}
		logv.SetLogger(logv.NewLogger(writers...))
		return nil
	}
	cmdMain.Command = func() error {
		err := a.Init()
		if err != nil {
			return err
		}
		defer a.Exit()
		event.Start()
		server, err := NewServer(WithHost(opts.Host), WithPort(opts.Port))
		if err != nil {
			return err
		}
		server.SetRouter(a.Router())

		sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		runErr := make(chan error, 1)
		go func() {
			runErr <- server.Run()
		}()

		select {
		case err := <-runErr:
			event.Stop()
			return err
		case <-sigCtx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), server.config.ShutdownTimeout)
			defer cancel()

			shutdownErr := server.Shutdown(shutdownCtx)
			event.Stop()

			err := <-runErr
			if shutdownErr != nil && !errors.Is(shutdownErr, context.Canceled) {
				return shutdownErr
			}
			return err
		}
	}
	if err := cmdMain.Parse(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	return cmdMain.Run()
}
