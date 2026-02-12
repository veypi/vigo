package vigo

import (
	"mime/multipart"
	"reflect"
	"testing"
)

type DocTestUser struct {
	ID   string `json:"id" desc:"User ID"`
	Name string `json:"name" desc:"User Name"`
}

type DocTestReq struct {
	ID     string                `src:"path@id"`
	Query  string                `src:"query"`
	Name   string                `json:"name"`
	Avatar *multipart.FileHeader `src:"form" desc:"Avatar file"`
}

type DocBaseReq struct {
	Page int `src:"query" desc:"Page number"`
}

type DocAnonReq struct {
	DocBaseReq
	Size int    `src:"query" desc:"Page size"`
	Name string `json:"name"`
}

type DocNamedReq struct {
	Base DocBaseReq  `json:"base"`
	User DocTestUser `json:"user"`
}

func TestParseDocArgs(t *testing.T) {
	reqType := reflect.TypeOf(DocTestReq{})
	params, body := parseDocArgs(reqType)

	// Check Params
	// Expect 2 params: ID (path), Query (query)
	if len(params) != 2 {
		t.Errorf("Expected 2 params, got %d", len(params))
	}

	// Check ID param
	foundID := false
	for _, p := range params {
		if p.Name == "id" && p.In == "path" {
			foundID = true
			break
		}
	}
	if !foundID {
		t.Error("Expected param id in path")
	}

	// Check Body
	// Expect Name (json/form) and Avatar (form)
	if body == nil {
		t.Fatal("Expected body, got nil")
	}
	if body.ContentType != "multipart/form-data" {
		t.Errorf("Expected multipart/form-data, got %s", body.ContentType)
	}
	if body.Type != "object" {
		t.Errorf("Expected object type, got %s", body.Type)
	}
	if len(body.Fields) != 2 {
		t.Errorf("Expected 2 body fields, got %d", len(body.Fields))
	}
}

func TestParseDocArgs_Anonymous(t *testing.T) {
	reqType := reflect.TypeOf(DocAnonReq{})
	params, body := parseDocArgs(reqType)

	// Expect 2 query params: Page (from embedded) and Size
	if len(params) != 2 {
		t.Errorf("Expected 2 params, got %d", len(params))
	}
	foundPage := false
	foundSize := false
	for _, p := range params {
		if p.Name == "Page" && p.In == "query" {
			foundPage = true
		}
		if p.Name == "Size" && p.In == "query" {
			foundSize = true
		}
	}
	if !foundPage {
		t.Error("Expected embedded param Page")
	}
	if !foundSize {
		t.Error("Expected param Size")
	}

	// Expect 1 body field: Name
	// (DocBaseReq fields should NOT be in body because they are src:"query")
	// Wait, if DocBaseReq had json fields, they should be here too flattened.
	// But DocBaseReq only has Page (query).
	// So only Name should be in body.
	if body == nil {
		t.Fatal("Expected body")
	}
	if len(body.Fields) != 1 {
		t.Errorf("Expected 1 body field, got %d", len(body.Fields))
	}
	if body.Fields[0].Name != "name" {
		t.Errorf("Expected field name, got %s", body.Fields[0].Name)
	}
}

func TestParseDocArgs_Named(t *testing.T) {
	reqType := reflect.TypeOf(DocNamedReq{})
	params, body := parseDocArgs(reqType)

	// DocNamedReq has Base (json) and User (json).
	// No top level params (unless recursively found? No, they are json fields).
	if len(params) != 0 {
		t.Errorf("Expected 0 params, got %d", len(params))
	}

	if body == nil {
		t.Fatal("Expected body")
	}
	// Expect 2 fields: base, user
	if len(body.Fields) != 2 {
		t.Errorf("Expected 2 body fields, got %d", len(body.Fields))
	}
	names := make(map[string]bool)
	for _, f := range body.Fields {
		names[f.Name] = true
	}
	if !names["base"] {
		t.Error("Expected field base")
	}
	if !names["user"] {
		t.Error("Expected field user")
	}
}

func TestParseDocResponse(t *testing.T) {
	// 1. Struct (Object)
	respType := reflect.TypeOf(DocTestUser{})
	body := parseDocResponse(respType)
	if body.Type != "object" {
		t.Errorf("Expected object type for struct, got %s", body.Type)
	}
	if len(body.Fields) != 2 {
		t.Errorf("Expected 2 fields for User, got %d", len(body.Fields))
	}

	// 2. Slice of Structs (Array)
	respType = reflect.TypeOf([]DocTestUser{})
	body = parseDocResponse(respType)
	if body.Type != "array" {
		t.Errorf("Expected array type for slice, got %s", body.Type)
	}
	if body.Item == nil {
		t.Fatal("Expected Item for array, got nil")
	}
	if body.Item.Type != "object" {
		t.Errorf("Expected Item type object, got %s", body.Item.Type)
	}
	if len(body.Item.Fields) != 2 {
		t.Errorf("Expected 2 fields in Item, got %d", len(body.Item.Fields))
	}

	// 3. Slice of Primitives (Array)
	respType = reflect.TypeOf([]string{})
	body = parseDocResponse(respType)
	if body.Type != "array" {
		t.Errorf("Expected array type for slice of strings, got %s", body.Type)
	}
	if body.Item == nil {
		t.Fatal("Expected Item for array, got nil")
	}
	if body.Item.Type != "string" {
		t.Errorf("Expected Item type string, got %s", body.Item.Type)
	}

	// 4. Primitive
	respType = reflect.TypeOf(123)
	body = parseDocResponse(respType)
	if body.Type != "int" {
		t.Errorf("Expected int type, got %s", body.Type)
	}
}

func TestDocIntegration(t *testing.T) {
	r := NewRouter()

	// Handler 1: Returns Struct
	r.Get("/user", "Get User", func(x *X) (*DocTestUser, error) {
		return &DocTestUser{}, nil
	})

	// Handler 2: Returns Slice of Structs
	r.Get("/users", "Get Users", func(x *X) ([]*DocTestUser, error) {
		return []*DocTestUser{}, nil
	})

	// Handler 3: Returns Primitive
	r.Get("/version", "Get Version", func(x *X) (string, error) {
		return "1.0.0", nil
	})

	// Handler 4: Returns Slice of Primitives
	r.Get("/tags", "Get Tags", func(x *X) ([]string, error) {
		return []string{"a", "b"}, nil
	})

	doc := r.Doc()

	findRoute := func(doc *Doc, path, method string) *DocRoute {
		for _, r := range doc.Routes {
			if r.Path == path && r.Method == method {
				return r
			}
		}
		return nil
	}

	// Check /user
	routeUser := findRoute(doc, "/user", "GET")
	if routeUser == nil {
		var paths []string
		for _, r := range doc.Routes {
			paths = append(paths, r.Method+" "+r.Path)
		}
		t.Fatalf("Route /user not found. Available: %v", paths)
	}
	if routeUser.Response == nil {
		t.Error("Route /user response is nil")
	} else if routeUser.Response.Type != "object" {
		t.Errorf("Route /user response type expected object, got %s", routeUser.Response.Type)
	}

	// Check /users
	routeUsers := findRoute(doc, "/users", "GET")
	if routeUsers == nil {
		t.Fatal("Route /users not found")
	}
	if routeUsers.Response == nil {
		// Expect this to fail initially
		t.Error("Route /users response is nil")
	} else if routeUsers.Response.Type != "array" {
		t.Errorf("Route /users response type expected array, got %s", routeUsers.Response.Type)
	}

	// Check /version
	routeVersion := findRoute(doc, "/version", "GET")
	if routeVersion == nil {
		t.Fatal("Route /version not found")
	}
	if routeVersion.Response == nil {
		// Expect this to fail initially
		t.Error("Route /version response is nil")
	} else if routeVersion.Response.Type != "string" {
		t.Errorf("Route /version response type expected string, got %s", routeVersion.Response.Type)
	}
}
