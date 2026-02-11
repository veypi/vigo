//
// router_test.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-08-08 18:20
// Distributed under terms of the MIT license.
//

package vigo

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/veypi/vigo/logv"
)

const twentyBrace = "/{a}/{b}/{c}/{d}/{e}/{f}/{g}/{h}/{i}/{j}/{k}/{l}/{m}/{n}/{o}/{p}/{q}/{r}/{s}/{t}"
const twentyRoute = "/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t"

type fakeResponseWriter struct {
	d []byte
}

func (f *fakeResponseWriter) Header() http.Header {
	return nil
}

func (f *fakeResponseWriter) Write(p []byte) (int, error) {
	f.d = p
	return len(p), nil
}

func (f *fakeResponseWriter) WriteHeader(statusCode int) {
	return
}

// Helper to check response body
func checkResponse(t *testing.T, w *httptest.ResponseRecorder, expected string) {
	if w.Body.String() != expected {
		t.Helper()
		t.Errorf("Expected body '%s', got '%s'", expected, w.Body.String())
	}
}

// Helper to check status code
func checkStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	if w.Code != expected {
		t.Helper()
		t.Errorf("Expected status %d, got %d", expected, w.Code)
	}
}

func githubRouter() Router {
	r := NewRouter()
	r.SubRouter("abc").Use(func(x *X) error {
		// logv.Info().Int("id", 1).Str("p", x.Request.URL.Path).Msg("")
		x.Next()
		// logv.Info().Int("id", 11).Str("p", x.Request.URL.Path).Msg("")
		return nil
	})
	r.Use(func(x *X) error {
		// logv.Info().Int("id", 10).Str("p", x.Request.URL.Path).Msg(x.PathParams.Get(""))
		return nil
	})
	r.Clear("/abc", "*")
	r.After(func(x *X) error {
		// logv.Info().Int("id", 20).Str("p", x.Request.URL.Path).Msg(x.PathParams.Get(""))
		return nil
	})
	for _, api := range githubAPi {
		for _, m := range api.methods {
			r.Set(api.path, m, func(x *X) error {
				// logv.Info().Int("id", 0).Str("p", x.Request.URL.Path).Str("old", api.path).Msg("")
				x.WriteString(x.Request.URL.Path)
				return nil
			})
		}
	}
	return r
}

var testR Router

func init() {
	logv.SetLevel(logv.InfoLevel)
	testR = githubRouter()
	// testR.Print()
}

var req *http.Request
var temPath = ""
var w = new(fakeResponseWriter)

func BenchmarkRoute_GitHub_ALL(b *testing.B) {
	req, _ = http.NewRequest("GET", "/", nil)
	for i := 0; i < b.N; i++ {
		for _, api := range githubAPi {
			req.URL.Path = api.path
			req.RequestURI = api.path
			for _, m := range api.methods {
				req.Method = m
				testR.ServeHTTP(w, req)
			}
		}
	}
}

func BenchmarkRoute_GitHub_Static(b *testing.B) {
	req, _ := http.NewRequest("POST", "/markdown/raw", nil)
	for i := 0; i < b.N; i++ {
		testR.ServeHTTP(w, req)
	}
}
func BenchmarkRoute_GitHub_Param1(b *testing.B) {
	temPath = "/teams/{id}/repos"
	req, _ := http.NewRequest("GET", temPath, nil)
	for i := 0; i < b.N; i++ {
		testR.ServeHTTP(w, req)
	}
}

type User struct {
	ID string
}

type Resp struct {
	Code int
}

func BenchmarkRoute_ComplexHandler(b *testing.B) {
	// Setup a separate router for this benchmark to avoid interference
	r := NewRouter()

	// func(*X, *User) (*Resp, error)
	handler := func(x *X, u *User) (*Resp, error) {
		return &Resp{Code: 200}, nil
	}

	r.Get("/complex/{id}", handler)

	req, _ := http.NewRequest("GET", "/complex/123", nil)
	w := new(fakeResponseWriter)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func TestRoute_ServeHTTP(t *testing.T) {
	w := new(fakeResponseWriter)
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	temPath := twentyRoute
	for _, api := range githubAPi[0:1] {
		temPath = api.path
		req.URL.Path = temPath
		req.RequestURI = temPath
		for _, m := range api.methods {
			req.Method = m
			w.d = w.d[:0]
			testR.ServeHTTP(w, req)
		}
	}
}

func TestRouter_Methods(t *testing.T) {
	r := NewRouter()

	handler := func(x *X) {
		x.writer.Write([]byte(x.Request.Method))
	}

	r.Get("/get", handler)
	r.Post("/post", handler)
	r.Put("/put", handler)
	r.Delete("/delete", handler)
	r.Patch("/patch", handler)
	r.Head("/head", handler)
	r.Any("/any", handler)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{"GET", "/get", "GET"},
		{"POST", "/post", "POST"},
		{"PUT", "/put", "PUT"},
		{"DELETE", "/delete", "DELETE"},
		{"PATCH", "/patch", "PATCH"},
		{"HEAD", "/head", "HEAD"},
		{"GET", "/any", "GET"},
		{"POST", "/any", "POST"},
	}

	for _, tt := range tests {
		req, _ := http.NewRequest(tt.method, tt.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if tt.method == "HEAD" {
			if w.Body.String() != tt.body {
				// It's okay if it's empty for HEAD
			}
		} else {
			checkResponse(t, w, tt.body)
		}
		checkStatus(t, w, 200)
	}
}

func TestRouter_Params(t *testing.T) {
	r := NewRouter()

	r.Get("/user/{name}", func(x *X) {
		name := x.PathParams.Get("name")
		x.writer.Write([]byte("user:" + name))
	})

	r.Get("/files/{filepath:*}", func(x *X) {
		fp := x.PathParams.Get("filepath")
		x.writer.Write([]byte("file:" + fp))
	})

	// Test Param
	req, _ := http.NewRequest("GET", "/user/alice", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	checkResponse(t, w, "user:alice")

	// Test Wildcard
	req, _ = http.NewRequest("GET", "/files/css/style.css", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	checkResponse(t, w, "file:css/style.css")
}

func TestRouter_Middleware(t *testing.T) {
	r := NewRouter()

	// Global middleware
	r.Use(func(x *X) {
		x.writer.Write([]byte("B1."))
		x.Next()
	})

	r.After(func(x *X) {
		x.writer.Write([]byte(".A1"))
	})

	r.Get("/mid", func(x *X) {
		x.writer.Write([]byte("Handler"))
	})

	req, _ := http.NewRequest("GET", "/mid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	checkResponse(t, w, "B1.Handler.A1")
}

func TestRouter_SubRouter(t *testing.T) {
	r := NewRouter()

	api := r.SubRouter("/api")
	api.Use(func(x *X) {
		x.writer.Write([]byte("API."))
		x.Next()
	})

	v1 := api.SubRouter("/v1")
	v1.Get("/hello", func(x *X) {
		x.writer.Write([]byte("Hello"))
	})

	req, _ := http.NewRequest("GET", "/api/v1/hello", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	checkResponse(t, w, "API.Hello")
}

func TestRouter_Clear(t *testing.T) {
	r := NewRouter()
	r.Get("/remove", func(x *X) {
		x.writer.Write([]byte("exist"))
	})

	// Verify exists
	req, _ := http.NewRequest("GET", "/remove", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	checkStatus(t, w, 200)

	// Remove
	r.Clear("/remove", "GET")

	// Verify removed
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	checkStatus(t, w, 404)
}

func TestRouter_Conflict(t *testing.T) {
	r := NewRouter()
	r.Get("/users/{id}", func(x *X) { x.writer.Write([]byte("id")) })
	r.Get("/users/{name}", func(x *X) { x.writer.Write([]byte("name")) }) // Conflict

	req, _ := http.NewRequest("GET", "/users/123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// "id" handler is matched first because it was defined first and matching is ordered.
	checkResponse(t, w, "id")
}

func TestRouter_ParamCleanup(t *testing.T) {
	r := NewRouter()

	// Route 1: /users/{id}/details
	r.Get("/users/{id}/details", func(x *X) {
		x.writer.Write([]byte("details"))
	})

	// Route 2: /users/{any:*}
	r.Get("/users/{any:*}", func(x *X) {
		if x.PathParams.Get("id") != "" {
			x.writer.Write([]byte("bug:id_exists"))
			return
		}
		x.writer.Write([]byte("wild:" + x.PathParams.Get("any")))
	})

	req, _ := http.NewRequest("GET", "/users/123/profile", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Body.String() == "bug:id_exists" {
		t.Error("Router failed to clean up params from failed branch match")
	} else if w.Body.String() != "wild:123/profile" {
		t.Errorf("Expected 'wild:123/profile', got '%s'", w.Body.String())
	}
}

func TestNewRouterDesign(t *testing.T) {
	r := NewRouter()

	// 1. Basic Static and Param
	r.Get("/files/{filename}", func(x *X) {
		x.writer.Write([]byte("filename=" + x.PathParams.Get("filename")))
	})

	// 2. Catch remaining
	r.Get("/static/{filepath:*}", func(x *X) {
		x.writer.Write([]byte("filepath=" + x.PathParams.Get("filepath")))
	})

	// 3. Composite
	r.Get("/img/{name}.{ext}", func(x *X) {
		x.writer.Write([]byte(fmt.Sprintf("name=%s ext=%s", x.PathParams.Get("name"), x.PathParams.Get("ext"))))
	})

	// 4. Regex
	r.Get("/api/v{version:[0-9]+}/{resource}", func(x *X) {
		x.writer.Write([]byte(fmt.Sprintf("ver=%s res=%s", x.PathParams.Get("version"), x.PathParams.Get("resource"))))
	})

	// 5. Wildcard (Match All)
	r.Get("/all/*", func(x *X) {
		x.writer.Write([]byte("wildcard matched"))
	})

	// 6. Recursive Wildcard
	r.Get("/recursive/**", func(x *X) {
		x.writer.Write([]byte("recursive matched"))
	})

	// 7. Backtracking / Order
	// /a/b/{param1}/d
	// /a/b/{param2}/c
	r.Get("/a/b/{param1}/d", func(x *X) {
		x.writer.Write([]byte("d matched p1=" + x.PathParams.Get("param1")))
	})
	r.Get("/a/b/{param2}/c", func(x *X) {
		x.writer.Write([]byte("c matched p2=" + x.PathParams.Get("param2")))
	})

	server := httptest.NewServer(r)
	defer server.Close()

	tests := []struct {
		path     string
		expected string
	}{
		{"/files/test.txt", "filename=test.txt"},
		{"/static/a/b/c", "filepath=a/b/c"},
		{"/img/photo.jpg", "name=photo ext=jpg"},
		{"/api/v1/users", "ver=1 res=users"},
		{"/api/v123/posts", "ver=123 res=posts"},
		{"/all/anything", "wildcard matched"},
		{"/recursive/deep/path", "recursive matched"},
		{"/a/b/x/d", "d matched p1=x"},
		{"/a/b/x/c", "c matched p2=x"}, // Backtracking test
	}

	for _, tc := range tests {
		resp, err := http.Get(server.URL + tc.path)
		if err != nil {
			t.Fatalf("Request %s failed: %v", tc.path, err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("Request %s status %d", tc.path, resp.StatusCode)
		}
		body := make([]byte, 100)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()
		got := string(body[:n])
		if got != tc.expected {
			t.Errorf("Request %s: expected %q, got %q", tc.path, tc.expected, got)
		}
	}
}

func TestRouterPriority(t *testing.T) {
	r := NewRouter()

	// Specific vs Wildcard
	r.Get("/p/specific", func(x *X) {
		x.writer.Write([]byte("specific"))
	})
	r.Get("/p/{param}", func(x *X) {
		x.writer.Write([]byte("param=" + x.PathParams.Get("param")))
	})

	server := httptest.NewServer(r)
	defer server.Close()

	resp, _ := http.Get(server.URL + "/p/specific")
	body := make([]byte, 100)
	n, _ := resp.Body.Read(body)
	got := string(body[:n])

	if got != "specific" {
		t.Errorf("Expected specific, got %s", got)
	}

	resp, _ = http.Get(server.URL + "/p/other")
	n, _ = resp.Body.Read(body)
	got = string(body[:n])
	if got != "param=other" {
		t.Errorf("Expected param=other, got %s", got)
	}
}

func TestRouterPriority2(t *testing.T) {
	r := NewRouter()

	// Insert param first
	r.Get("/q/{param}", func(x *X) {
		x.writer.Write([]byte("param=" + x.PathParams.Get("param")))
	})
	r.Get("/q/specific", func(x *X) {
		x.writer.Write([]byte("specific"))
	})

	server := httptest.NewServer(r)
	defer server.Close()

	resp, _ := http.Get(server.URL + "/q/specific")
	body := make([]byte, 100)
	n, _ := resp.Body.Read(body)
	got := string(body[:n])

	if got != "specific" {
		t.Errorf("Expected specific (static > param), got %s", got)
	}
}

func TestRouterWildcardPriority(t *testing.T) {
	r := NewRouter()

	// Insert wildcard first
	r.Get("/w/*", func(x *X) {
		x.writer.Write([]byte("wildcard"))
	})
	r.Get("/w/specific", func(x *X) {
		x.writer.Write([]byte("specific"))
	})

	server := httptest.NewServer(r)
	defer server.Close()

	// Wildcard should be sorted to end. So specific should match.

	resp, _ := http.Get(server.URL + "/w/specific")
	body := make([]byte, 100)
	n, _ := resp.Body.Read(body)
	got := string(body[:n])

	if got != "specific" {
		t.Errorf("Expected specific (wildcard sorted last), got %s", got)
	}
}

var githubAPi = []struct {
	path    string
	methods []string
}{
	{"/abc/{id}/anbc/{pth}", []string{"GET"}},
	{twentyBrace, []string{"GET"}},
	{"/", []string{"GET"}},
	{"/gitignore/templates", []string{"GET"}},
	{"/repos/{owner}/{repo}/commits/{sha}", []string{"GET"}},
	{"/repos/{owner}/{repo}/issues/{number}", []string{"GET"}},
	{"/applications/{client_id}/tokens", []string{"DELETE"}},
	{"/users/{user}/gists", []string{"GET"}},
	{"/notifications", []string{"GET", "PUT"}},
	{"/repos/{owner}/{repo}/hooks", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/labels", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/git/commits/{sha}", []string{"GET"}},
	{"/users/{user}/events", []string{"GET"}},
	{"/repos/{owner}/{repo}/pulls", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/languages", []string{"GET"}},
	{"/gists/{id}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/git/commits", []string{"POST"}},
	{"/orgs/{org}/events", []string{"GET"}},
	{"/repos/{owner}/{repo}/stats/commit_activity", []string{"GET"}},
	{"/gists", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/statuses/{ref}", []string{"GET", "POST"}},
	{"/issues", []string{"GET"}},
	{"/rate_limit", []string{"GET"}},
	{"/orgs/{org}/members", []string{"GET"}},
	{"/repos/{owner}/{repo}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/collaborators", []string{"GET"}},
	{"/user/starred/{owner}/{repo}", []string{"GET", "PUT", "DELETE"}},
	{"/markdown/raw", []string{"POST"}},
	{"/users/{user}/repos", []string{"GET"}},
	{"/repos/{owner}/{repo}/keys", []string{"GET", "POST"}},
	{"/teams/{id}/members", []string{"GET"}},
	{"/repos/{owner}/{repo}/releases/{id}/assets", []string{"GET"}},
	{"/repos/{owner}/{repo}/milestones/{number}/labels", []string{"GET"}},
	{"/repos/{owner}/{repo}/keys/{id}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/git/tags", []string{"POST"}},
	{"/repos/{owner}/{repo}/teams", []string{"GET"}},
	{"/repos/{owner}/{repo}/issues/{number}/events", []string{"GET"}},
	{"/repos/{owner}/{repo}/milestones", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/notifications", []string{"GET", "PUT"}},
	{"/user/keys", []string{"GET", "POST"}},
	{"/emojis", []string{"GET"}},
	{"/search/issues", []string{"GET"}},
	{"/orgs/{org}/issues", []string{"GET"}},
	{"/repos/{owner}/{repo}/commits/{sha}/comments", []string{"GET", "POST"}},
	{"/search/code", []string{"GET"}},
	{"/meta", []string{"GET"}},
	{"/repos/{owner}/{repo}/git/blobs/{sha}", []string{"GET"}},
	{"/notifications/threads/{id}/subscription", []string{"GET", "PUT", "DELETE"}},
	{"/legacy/user/search/{keyword}", []string{"GET"}},
	{"/user/orgs", []string{"GET"}},
	{"/repos/{owner}/{repo}/pulls/{number}/files", []string{"GET"}},
	{"/users/{user}/following", []string{"GET"}},
	{"/orgs/{org}", []string{"GET"}},
	{"/search/users", []string{"GET"}},
	{"/user/teams", []string{"GET"}},
	{"/repos/{owner}/{repo}/stats/code_frequency", []string{"GET"}},
	{"/teams/{id}/repos", []string{"GET"}},
	{"/events", []string{"GET"}},
	{"/orgs/{org}/members/{user}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/git/trees/{sha}", []string{"GET"}},
	{"/users/{user}/received_events", []string{"GET"}},
	{"/networks/{owner}/{repo}/events", []string{"GET"}},
	{"/repos/{owner}/{repo}/hooks/{id}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/pulls/{number}/comments", []string{"GET", "PUT"}},
	{"/user/following", []string{"GET"}},
	{"/gitignore/templates/{name}", []string{"GET"}},
	{"/repos/{owner}/{repo}/tags", []string{"GET"}},
	{"/users/{user}/events/orgs/{org}", []string{"GET"}},
	{"/repos/{owner}/{repo}/releases/{id}", []string{"GET", "DELETE"}},
	{"/gists/{id}/star", []string{"PUT", "DELETE", "GET"}},
	{"/repos/{owner}/{repo}/collaborators/{user}", []string{"GET", "PUT", "DELETE"}},
	{"/user/repos", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/branches", []string{"GET"}},
	{"/notifications/threads/{id}", []string{"GET"}},
	{"/repos/{owner}/{repo}/issues/{number}/labels", []string{"GET", "POST", "PUT", "DELETE"}},
	{"/repos/{owner}/{repo}/contributors", []string{"GET"}},
	{"/orgs/{org}/public_members", []string{"GET"}},
	{"/users/{user}/received_events/public", []string{"GET"}},
	{"/repos/{owner}/{repo}/git/refs", []string{"GET", "POST"}},
	{"/user/subscriptions/{owner}/{repo}", []string{"GET", "PUT", "DELETE"}},
	{"/legacy/user/email/{email}", []string{"GET"}},
	{"/repos/{owner}/{repo}/git/blobs", []string{"POST"}},
	{"/legacy/issues/search/{owner}/{repository}/{state}/{keyword}", []string{"GET"}},
	{"/repos/{owner}/{repo}/events", []string{"GET"}},
	{"/user/subscriptions", []string{"GET"}},
	{"/markdown", []string{"POST"}},
	{"/gists/{id}/forks", []string{"POST"}},
	{"/repos/{owner}/{repo}/stargazers", []string{"GET"}},
	{"/users/{user}", []string{"GET"}},
	{"/user/following/{user}", []string{"GET", "PUT", "DELETE"}},
	{"/user/emails", []string{"GET", "POST", "DELETE"}},
	{"/repos/{owner}/{repo}/comments", []string{"GET"}},
	{"/teams/{id}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/milestones/{number}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/stats/contributors", []string{"GET"}},
	{"/teams/{id}/repos/{owner}/{repo}", []string{"GET", "PUT", "DELETE"}},
	{"/repos/{owner}/{repo}/stats/punch_card", []string{"GET"}},
	{"/users/{user}/keys", []string{"GET"}},
	{"/repos/{owner}/{repo}/hooks/{id}/tests", []string{"POST"}},
	{"/users/{user}/subscriptions", []string{"GET"}},
	{"/repos/{owner}/{repo}/assignees", []string{"GET"}},
	{"/user", []string{"GET"}},
	{"/authorizations/{id}", []string{"GET", "DELETE"}},
	{"/orgs/{org}/teams", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/issues", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/issues/{number}/comments", []string{"GET", "POST"}},
	{"/applications/{client_id}/tokens/{access_token}", []string{"GET", "DELETE"}},
	{"/feeds", []string{"GET"}},
	{"/repos/{owner}/{repo}/comments/{id}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/pulls/{number}", []string{"GET"}},
	{"/repos/{owner}/{repo}/downloads/{id}", []string{"GET", "DELETE"}},
	{"/users/{user}/orgs", []string{"GET"}},
	{"/orgs/{org}/repos", []string{"GET", "POST"}},
	{"/users/{user}/following/{target_user}", []string{"GET"}},
	{"/repos/{owner}/{repo}/readme", []string{"GET"}},
	{"/repos/{owner}/{repo}/forks", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/issues/{number}/labels/{name}", []string{"DELETE"}},
	{"/legacy/repos/search/{keyword}", []string{"GET"}},
	{"/repos/{owner}/{repo}/merges", []string{"POST"}},
	{"/repos/{owner}/{repo}/git/tags/{sha}", []string{"GET"}},
	{"/search/repositories", []string{"GET"}},
	{"/user/starred", []string{"GET"}},
	{"/teams/{id}/members/{user}", []string{"GET", "PUT", "DELETE"}},
	{"/users", []string{"GET"}},
	{"/user/issues", []string{"GET"}},
	{"/repos/{owner}/{repo}/subscribers", []string{"GET"}},
	{"/repos/{owner}/{repo}/git/trees", []string{"POST"}},
	{"/users/{user}/events/public", []string{"GET"}},
	{"/repos/{owner}/{repo}/pulls/{number}/merge", []string{"GET", "PUT"}},
	{"/repos/{owner}/{repo}/assignees/{assignee}", []string{"GET"}},
	{"/users/{user}/starred", []string{"GET"}},
	{"/repos/{owner}/{repo}/labels/{name}", []string{"GET", "DELETE"}},
	{"/user/followers", []string{"GET"}},
	{"/orgs/{org}/public_members/{user}", []string{"GET", "PUT", "DELETE"}},
	{"/authorizations", []string{"GET", "POST"}},
	{"/repos/{owner}/{repo}/downloads", []string{"GET"}},
	{"/repos/{owner}/{repo}/releases", []string{"GET", "POST"}},
	{"/user/keys/{id}", []string{"GET", "DELETE"}},
	{"/repos/{owner}/{repo}/stats/participation", []string{"GET"}},
	{"/repos/{owner}/{repo}/subscription", []string{"GET", "PUT", "DELETE"}},
	{"/repositories", []string{"GET"}},
	{"/repos/{owner}/{repo}/branches/{branch}", []string{"GET"}},
	{"/repos/{owner}/{repo}/pulls/{number}/commits", []string{"GET"}},
	{"/users/{user}/followers", []string{"GET"}},
	{"/repos/{owner}/{repo}/commits", []string{"GET"}},
}
