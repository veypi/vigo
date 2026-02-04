//
// cfg.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-08-12 12:08
// Distributed under terms of the MIT license.
//

package vigo

import (
	"crypto/tls"
	"errors"
	"fmt"
	"regexp"
)

var ipv4Regex = regexp.MustCompile(`^((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])$`)

type Config struct {
	DocPath string `json:"doc_path,omitempty"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	// log file path
	LoggerPath     string `json:"logger_path,omitempty"`
	LoggerLevel    string `json:"logger_level,omitempty"`
	PrettyLog      bool   `json:"pretty_log,omitempty"`
	TimeFormat     string `json:"time_format,omitempty"`
	PostMaxMemory  uint
	TlsCfg         *tls.Config
	MaxConnections int
	DisableReqLog  bool `json:"disable_req_log,omitempty"`
}

func (c *Config) Url() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) IsValid() error {
	if !ipv4Regex.MatchString(c.Host) {
		return errors.New("invalid host")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return errors.New("invalid port")
	}
	return nil
}

func WithTls(cfg *tls.Config) func(*Config) {
	return func(c *Config) {
		c.TlsCfg = cfg
	}
}

func WithDocPath(path string) func(*Config) {
	return func(c *Config) {
		c.DocPath = path
	}
}

func WithHost(host string) func(*Config) {
	return func(c *Config) {
		c.Host = host
	}
}

func WithPort(port int) func(*Config) {
	return func(c *Config) {
		c.Port = port
	}
}

func WithLoggerPath(path string) func(*Config) {
	return func(c *Config) {
		c.LoggerPath = path
	}
}

func WithLoggerLevel(level string) func(*Config) {
	return func(c *Config) {
		c.LoggerLevel = level
	}
}

func WithPrettyLog() func(*Config) {
	return func(c *Config) {
		c.PrettyLog = true
	}
}
