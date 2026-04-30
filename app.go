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
	"io"
	"os"
	"os/signal"
	"strconv"
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
	Run() error
}

func New[T any](name string, router Router, config T, init func() error) App[T] {
	return &app[T]{
		router: router,
		name:   name,
		cfg:    config,
		init:   init,
	}
}

type app[T any] struct {
	router Router
	name   string
	cfg    T
	init   func() error
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

func (a *app[T]) Run() error {
	godotenv.Load()
	cmdMain := flags.New(a.Name(), "")
	host := cmdMain.String("host", flags.LoadEnvOr("HOST", "0.0.0.0"), "host address (env: HOST)")
	port := cmdMain.Int("p", func() int {
		if v := os.Getenv("PORT"); v != "" {
			if i, err := strconv.Atoi(v); err == nil {
				return i
			}
		}
		return 4000
	}(), "port (env: PORT)")
	configFile := cmdMain.String("f", "./dev.yaml", "the config file")
	loggerLevel := cmdMain.String("l", "debug", "logger_level")
	loggerPath := cmdMain.String("logger_path", "", "logger_path")
	loggerMode := cmdMain.String("logger_mode", "console", "logger mode: console | nocolor | json")
	cmdCfg := cmdMain.SubCommand("gen", "generate cfg file")
	cmdCfg.Command = func() error {
		return flags.DumpCfg(*configFile, a.Config())
	}
	cmdMain.Before = func() error {
		flags.LoadCfg(*configFile, a.Config())
		cmdMain.Parse()
		logv.SetLevel(logv.AssertFuncErr(logv.ParseLevel(*loggerLevel)))
		var writers []io.Writer
		switch *loggerMode {
		case "nocolor":
			writers = append(writers, logv.ConsoleWriterNoColor())
		case "json":
			writers = append(writers, os.Stdout)
		default:
			writers = append(writers, logv.ConsoleWriter())
		}
		if loggerPath != nil && *loggerPath != "" {
			logv.FileHook.Filename = *loggerPath
			writers = append(writers, &logv.FileHook)
		}
		logv.SetLogger(logv.NewLogger(writers...))
		return nil
	}
	cmdMain.AutoRegister(a.Config())
	cmdMain.Command = func() error {
		err := a.init()
		if err != nil {
			return err
		}
		event.Start()
		server, err := NewServer(WithHost(*host), WithPort(*port))
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
	cmdMain.Parse()
	return cmdMain.Run()
}
