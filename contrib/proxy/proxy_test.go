//
// proxy_test.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func TestRuleUnmarshalJSON(t *testing.T) {
	var m map[string]*Rule
	data := `{
		"/api": "http://127.0.0.1:8000",
		"/auth": {"target": "http://127.0.0.1:9000", "rewrite": {"^/auth": ""}}
	}`
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatal(err)
	}
	if m["/api"].Target != "http://127.0.0.1:8000" {
		t.Fatalf("shorthand target = %q", m["/api"].Target)
	}
	if m["/auth"].Target != "http://127.0.0.1:9000" {
		t.Fatalf("object target = %q", m["/auth"].Target)
	}
	if m["/auth"].Rewrite["^/auth"] != "" {
		t.Fatalf("rewrite = %v", m["/auth"].Rewrite)
	}
}

// TestProxyBehavior 用真实后端验证 New 内部的转发语义：
// 路径保持、rewrite 去前缀、target 带 base 路径、changeOrigin 与 X-Forwarded-For。
func TestProxyBehavior(t *testing.T) {
	var gotPath, gotHost, gotXFF string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		gotXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	cases := []struct {
		name     string
		rule     *Rule
		inPath   string
		wantPath string
	}{
		{"prefix preserved", &Rule{Target: backend.URL}, "/api/users/1", "/api/users/1"},
		{"rewrite strip", &Rule{Target: backend.URL, Rewrite: map[string]string{"^/api": ""}}, "/api/users", "/users"},
		{"target with base", &Rule{Target: backend.URL + "/base"}, "/api/x", "/base/api/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target, res, keys, err := c.rule.compile()
			if err != nil {
				t.Fatal(err)
			}
			rp := &httputil.ReverseProxy{
				Rewrite: func(pr *httputil.ProxyRequest) {
					pr.SetURL(target)
					pr.SetXForwarded()
					for i, re := range res {
						pr.Out.URL.Path = re.ReplaceAllString(pr.Out.URL.Path, c.rule.Rewrite[keys[i]])
					}
				},
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://front"+c.inPath, nil)
			rp.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
			if gotPath != c.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, c.wantPath)
			}
			u, _ := url.Parse(backend.URL)
			if gotHost != u.Host {
				t.Fatalf("host = %q, want %q (changeOrigin)", gotHost, u.Host)
			}
			if gotXFF == "" {
				t.Fatal("missing X-Forwarded-For")
			}
		})
	}
}

func TestNewInvalid(t *testing.T) {
	if _, err := New(&Rule{}); err == nil {
		t.Fatal("empty target should fail")
	}
	if _, err := New(&Rule{Target: "127.0.0.1:8000"}); err == nil {
		t.Fatal("missing scheme should fail")
	}
	if _, err := New(&Rule{Target: "http://x", Rewrite: map[string]string{"([": ""}}); err == nil {
		t.Fatal("bad regexp should fail")
	}
}

func TestProxyToPanicsOnBadTarget(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ProxyTo with bad target should panic")
		}
	}()
	ProxyTo("noscheme")
}
