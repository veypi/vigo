//
// proxy.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

// Package proxy 提供反向代理 handler：把请求转发到目标后端，
// 支持路径正则重写，WebSocket Upgrade 由 httputil.ReverseProxy 自动处理。
package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sort"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/logv"
)

// Rule 一条代理规则。
// JSON 配置支持两种形态：
//
//	"/api": "http://127.0.0.1:8000"                          // 字符串简写 = 仅 Target
//	"/api": {"target": "http://127.0.0.1:8000", "rewrite": {"^/api": ""}}
type Rule struct {
	// Target 后端地址，必须是绝对 URL（scheme://host），可带路径前缀（转发时与请求路径拼接）
	Target string `json:"target"`
	// Rewrite 路径重写：正则表达式 → 替换内容，按 key 字典序依次应用。
	// 例：{"^/api": ""} 去除 /api 前缀。
	Rewrite map[string]string `json:"rewrite,omitempty"`
}

// UnmarshalJSON 支持字符串简写形态。
func (r *Rule) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		r.Target = s
		return nil
	}
	type alias Rule
	return json.Unmarshal(data, (*alias)(r))
}

// compile 校验 target 并编译 rewrite 规则（按 key 字典序保证应用顺序确定）。
func (r *Rule) compile() (*url.URL, []*regexp.Regexp, []string, error) {
	if r.Target == "" {
		return nil, nil, nil, fmt.Errorf("proxy: empty target")
	}
	target, err := url.Parse(r.Target)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("proxy: invalid target %q: %w", r.Target, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, nil, nil, fmt.Errorf("proxy: target %q must be an absolute URL (scheme://host)", r.Target)
	}
	keys := make([]string, 0, len(r.Rewrite))
	for k := range r.Rewrite {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	res := make([]*regexp.Regexp, 0, len(keys))
	for _, k := range keys {
		re, err := regexp.Compile(k)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("proxy: invalid rewrite pattern %q: %w", k, err)
		}
		res = append(res, re)
	}
	return target, res, keys, nil
}

// New 创建代理 handler。转发时 Host 头重写为 target 的 host（changeOrigin 语义），
// 并自动追加 X-Forwarded-For/X-Forwarded-Host 等转发头。
func New(rule *Rule) (func(*vigo.X), error) {
	target, res, keys, err := rule.compile()
	if err != nil {
		return nil, err
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			for i, re := range res {
				pr.Out.URL.Path = re.ReplaceAllString(pr.Out.URL.Path, rule.Rewrite[keys[i]])
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logv.WithNoCaller.Warn().Err(err).Str("target", rule.Target).Str("path", r.URL.Path).Msg("proxy upstream error")
			http.Error(w, "Bad Gateway: "+err.Error(), http.StatusBadGateway)
		},
	}
	return func(x *vigo.X) {
		rp.ServeHTTP(x.ResponseWriter(), x.Request)
	}, nil
}

// ProxyTo 简写形态：无路径重写的单目标代理，targetHost 非法时 panic。
func ProxyTo(targetHost string) func(*vigo.X) {
	h, err := New(&Rule{Target: targetHost})
	if err != nil {
		panic(err)
	}
	return h
}
