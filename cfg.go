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
	"net"
	"regexp"
	"time"
)

var ipv4Regex = regexp.MustCompile(`^((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])$`)

type Config struct {
	DocPath string `json:"doc_path,omitempty"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	// log file path
	LoggerPath        string        `json:"logger_path,omitempty"`
	LoggerLevel       string        `json:"logger_level,omitempty"`
	PrettyLog         bool          `json:"pretty_log,omitempty"`
	TimeFormat        string        `json:"time_format,omitempty"`
	PostMaxMemory     uint          `json:"post_max_memory,omitempty"`
	TlsCfg            *tls.Config   `json:"-"`
	MaxConnections    int           `json:"max_connections,omitempty"`
	ReadTimeout       time.Duration `json:"read_timeout,omitempty"`
	ReadHeaderTimeout time.Duration `json:"read_header_timeout,omitempty"`
	WriteTimeout      time.Duration `json:"write_timeout,omitempty"`
	IdleTimeout       time.Duration `json:"idle_timeout,omitempty"`
	ShutdownTimeout   time.Duration `json:"shutdown_timeout,omitempty"`
	MaxHeaderBytes    int           `json:"max_header_bytes,omitempty"`
	TrustedProxies    []string      `json:"trusted_proxies,omitempty"`
	RequestIDHeader   string        `json:"request_id_header,omitempty"`
	DisableReqLog     bool          `json:"disable_req_log,omitempty"`
}

func (c *Config) Url() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) IsValid() error {
	if c.Host == "" {
		return errors.New("invalid host")
	}
	if c.PostMaxMemory == 0 {
		c.PostMaxMemory = 32 << 20
	}
	// 超时配置默认为 0（无限制），由使用者自行设置
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 10 * time.Second
	}
	if c.MaxHeaderBytes <= 0 {
		c.MaxHeaderBytes = 1 << 20
	}
	if c.RequestIDHeader == "" {
		c.RequestIDHeader = "X-Request-ID"
	}
	if !ipv4Regex.MatchString(c.Host) && net.ParseIP(c.Host) == nil && !isHostname(c.Host) && c.Host != "0.0.0.0" && c.Host != "::" {
		return errors.New("invalid host")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return errors.New("invalid port")
	}
	return nil
}

func isHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range regexp.MustCompile(`\.`).Split(host, -1) {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		for i, r := range label {
			isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !isAlphaNum && r != '-' {
				return false
			}
			if (i == 0 || i == len(label)-1) && r == '-' {
				return false
			}
		}
	}
	return true
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

func WithReadTimeout(timeout time.Duration) func(*Config) {
	return func(c *Config) {
		c.ReadTimeout = timeout
	}
}

func WithReadHeaderTimeout(timeout time.Duration) func(*Config) {
	return func(c *Config) {
		c.ReadHeaderTimeout = timeout
	}
}

func WithWriteTimeout(timeout time.Duration) func(*Config) {
	return func(c *Config) {
		c.WriteTimeout = timeout
	}
}

func WithIdleTimeout(timeout time.Duration) func(*Config) {
	return func(c *Config) {
		c.IdleTimeout = timeout
	}
}

func WithShutdownTimeout(timeout time.Duration) func(*Config) {
	return func(c *Config) {
		c.ShutdownTimeout = timeout
	}
}

func WithMaxHeaderBytes(size int) func(*Config) {
	return func(c *Config) {
		c.MaxHeaderBytes = size
	}
}

func WithTrustedProxies(proxies ...string) func(*Config) {
	return func(c *Config) {
		c.TrustedProxies = append(c.TrustedProxies[:0], proxies...)
	}
}

func WithRequestIDHeader(header string) func(*Config) {
	return func(c *Config) {
		c.RequestIDHeader = header
	}
}
