package vigo

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Helper to create a basic X context
func createTestX(method, urlStr string, body interface{}) (*X, error) {
	var req *http.Request
	var err error

	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		req, err = http.NewRequest(method, urlStr, bytes.NewBuffer(jsonBytes))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequest(method, urlStr, nil)
	}

	if err != nil {
		return nil, err
	}

	x := acquire()
	x.Request = req
	x.writer = httptest.NewRecorder()
	return x, nil
}

func TestParseJSON(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	payload := User{Name: "Alice", Age: 30}
	x, _ := createTestX("POST", "/", payload)
	defer release(x)

	var target User
	err := x.Parse(&target)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if target.Name != payload.Name || target.Age != payload.Age {
		t.Errorf("Expected %+v, got %+v", payload, target)
	}
}

func TestParseEmptyVsMissing(t *testing.T) {
	// struct with both required (non-pointer) and optional (pointer) fields
	// for all 4 sources
	type ComplexReq struct {
		// Query
		QReq string  `src:"query"`
		QOpt *string `src:"query"`

		// Header
		HReq string  `src:"header"`
		HOpt *string `src:"header"`

		// Form
		FReq string  `src:"form"`
		FOpt *string `src:"form"`

		// Path
		PReq string  `src:"path"`
		POpt *string `src:"path"`
	}

	// Helper to populate a request with ALL required fields set to empty strings
	// This serves as a base for "Missing" tests (we remove one) and "Empty" tests (we keep all)
	setupBaseReq := func() *X {
		// 1. Query
		// 2. Form (requires body)
		// 3. Header
		// 4. Path (requires X.PathParams)

		// Create request with Query and Body
		// We use empty strings for values
		// Note: Field names are snake_cased by default if no alias provided
		u, _ := url.Parse("/?q_req=")

		form := url.Values{}
		form.Add("f_req", "")

		req, _ := http.NewRequest("POST", u.String(), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		// Set Header
		req.Header.Set("h_req", "")

		x := acquire()
		x.Request = req
		x.writer = httptest.NewRecorder()

		// Set Path
		x.PathParams = PathParams{
			{Key: "p_req", Value: ""},
		}

		return x
	}

	// 1. Test Empty Values -> Success (Required fields should accept empty string)
	{
		x := setupBaseReq()
		// Also add optional fields as empty to ensure they work too
		q := x.Request.URL.Query()
		q.Add("q_opt", "")
		x.Request.URL.RawQuery = q.Encode()

		x.Request.Header.Set("h_opt", "")

		// Append to form body
		// Note: Reading body again requires creating new reader as previous one might be consumed if we didn't use createTestX helper correctly or if Parse consumes it.
		// Since setupBaseReq creates a fresh request with body, we can just recreate the body with more params.
		form := url.Values{}
		form.Add("f_req", "")
		form.Add("f_opt", "")
		x.Request.Body = io.NopCloser(strings.NewReader(form.Encode()))

		x.PathParams = append(x.PathParams, Param{Key: "p_opt", Value: ""})

		defer release(x)

		var target ComplexReq
		err := x.Parse(&target)
		if err != nil {
			t.Errorf("Empty Values Case failed: %v", err)
		}

		// Verify values are present (empty string) not nil or default
		if target.QReq != "" {
			t.Errorf("QReq expected empty, got %s", target.QReq)
		}
		if target.HReq != "" {
			t.Errorf("HReq expected empty, got %s", target.HReq)
		}
		if target.FReq != "" {
			t.Errorf("FReq expected empty, got %s", target.FReq)
		}
		if target.PReq != "" {
			t.Errorf("PReq expected empty, got %s", target.PReq)
		}

		// Optional fields should be present (pointer not nil) and point to empty string
		if target.QOpt == nil || *target.QOpt != "" {
			t.Errorf("QOpt expected ptr to empty, got %v", target.QOpt)
		}
		if target.HOpt == nil || *target.HOpt != "" {
			t.Errorf("HOpt expected ptr to empty, got %v", target.HOpt)
		}
		if target.FOpt == nil || *target.FOpt != "" {
			t.Errorf("FOpt expected ptr to empty, got %v", target.FOpt)
		}
		if target.POpt == nil || *target.POpt != "" {
			t.Errorf("POpt expected ptr to empty, got %v", target.POpt)
		}
	}

	// 2. Test Missing Required -> Error
	// For each required field, we create a valid base request and remove JUST that field.
	tests := []struct {
		name      string
		remove    func(*X)
		expectErr string // Field name expected in error
	}{
		{
			name: "Missing Query Required",
			remove: func(x *X) {
				// Rebuild query without q_req
				q := x.Request.URL.Query()
				q.Del("q_req")
				x.Request.URL.RawQuery = q.Encode()
			},
			expectErr: "q_req",
		},
		{
			name: "Missing Header Required",
			remove: func(x *X) {
				x.Request.Header.Del("h_req")
			},
			expectErr: "h_req",
		},
		{
			name: "Missing Form Required",
			remove: func(x *X) {
				// Recreate body without f_req
				// Base has f_req=""
				form := url.Values{}
				// form.Add("f_req", "") // Don't add
				x.Request.Body = io.NopCloser(strings.NewReader(form.Encode()))
			},
			expectErr: "f_req",
		},
		{
			name: "Missing Path Required",
			remove: func(x *X) {
				// Rebuild path params without p_req
				x.PathParams = PathParams{}
			},
			expectErr: "p_req",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := setupBaseReq()
			defer release(x)
			tt.remove(x)

			var target ComplexReq
			err := x.Parse(&target)
			if err == nil {
				t.Errorf("%s: Expected error but got nil", tt.name)
			} else {
				// Check if error contains field name
				// We expect ErrArgMissing or similar, usually containing the field name
				if !strings.Contains(err.Error(), tt.expectErr) {
					t.Errorf("%s: Expected error to contain '%s', got '%v'", tt.name, tt.expectErr, err)
				}
			}
		})
	}
}

func TestParseQuery(t *testing.T) {
	type QueryReq struct {
		Page int    `src:"query"`
		Sort string `src:"query"`
	}

	x, _ := createTestX("GET", "/?page=2&sort=desc", nil)
	defer release(x)

	var target QueryReq
	err := x.Parse(&target)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if target.Page != 2 {
		t.Errorf("Expected Page=2, got %d", target.Page)
	}
	if target.Sort != "desc" {
		t.Errorf("Expected Sort='desc', got '%s'", target.Sort)
	}
}

func TestParseForm(t *testing.T) {
	type FormReq struct {
		Username string `src:"form"`
		Active   bool   `src:"form"`
	}

	form := url.Values{}
	form.Add("username", "bob")
	form.Add("active", "true")

	req, _ := http.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	x := acquire()
	x.Request = req
	x.writer = httptest.NewRecorder()
	defer release(x)

	var target FormReq
	err := x.Parse(&target)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if target.Username != "bob" {
		t.Errorf("Expected Username='bob', got '%s'", target.Username)
	}
	if !target.Active {
		t.Errorf("Expected Active=true, got %v", target.Active)
	}
}

func TestParsePath(t *testing.T) {
	type PathReq struct {
		ID   int    `src:"path"`
		Slug string `src:"path"`
	}

	x, _ := createTestX("GET", "/users/123/profile", nil)
	defer release(x)

	// Manually set path params as if router matched them
	x.PathParams = PathParams{Param{Key: "id", Value: "123"}, Param{Key: "slug", Value: "profile"}}

	var target PathReq
	err := x.Parse(&target)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if target.ID != 123 {
		t.Errorf("Expected ID=123, got %d", target.ID)
	}
	if target.Slug != "profile" {
		t.Errorf("Expected Slug='profile', got '%s'", target.Slug)
	}
}

func TestParseHeader(t *testing.T) {
	type HeaderReq struct {
		AuthToken string `src:"header@X-Auth-Token"`
		UserAgent string `src:"header@User-Agent"`
	}

	x, _ := createTestX("GET", "/", nil)
	x.Request.Header.Set("X-Auth-Token", "secret123")
	x.Request.Header.Set("User-Agent", "TestAgent")
	defer release(x)

	var target HeaderReq
	err := x.Parse(&target)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if target.AuthToken != "secret123" {
		t.Errorf("Expected AuthToken='secret123', got '%s'", target.AuthToken)
	}
	if target.UserAgent != "TestAgent" {
		t.Errorf("Expected UserAgent='TestAgent', got '%s'", target.UserAgent)
	}
}

func TestParseDefault(t *testing.T) {
	type DefaultReq struct {
		Page    int    `src:"query" default:"1"`
		Keyword string `src:"query" default:"golang"`
	}

	x, _ := createTestX("GET", "/", nil) // No query params
	defer release(x)

	var target DefaultReq
	err := x.Parse(&target)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if target.Page != 1 {
		t.Errorf("Expected Page=1, got %d", target.Page)
	}
	if target.Keyword != "golang" {
		t.Errorf("Expected Keyword='golang', got '%s'", target.Keyword)
	}
}

func TestParseMix(t *testing.T) {
	type MixReq struct {
		ID    int    `src:"path"`
		Page  int    `src:"query"`
		Title string `json:"title"`
	}

	payload := map[string]string{"title": "Hello"}
	x, _ := createTestX("POST", "/posts/99?page=5", payload)
	defer release(x)
	x.PathParams = PathParams{Param{Key: "id", Value: "99"}}

	var target MixReq
	err := x.Parse(&target)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if target.ID != 99 {
		t.Errorf("Expected ID=99, got %d", target.ID)
	}
	if target.Page != 5 {
		t.Errorf("Expected Page=5, got %d", target.Page)
	}
	if target.Title != "Hello" {
		t.Errorf("Expected Title='Hello', got '%s'", target.Title)
	}
}
