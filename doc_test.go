package vigo

import (
	"encoding/json"
	"mime/multipart"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type DocReq struct {
	ID    int                   `json:"id" src:"path" desc:"user id"`
	Query string                `json:"q" src:"query" desc:"search query"`
	File  *multipart.FileHeader `json:"file" src:"form" desc:"upload file"`
}

type DocResp struct {
	Data string `json:"data"`
}

func TestDocGeneration(t *testing.T) {
	r := NewRouter()
	r.Post("/simple/{id}", "Simple Handler", func(x *X, req *DocReq) (*DocResp, error) {
		return nil, nil
	})

	doc := r.Doc()

	if doc.Title != "Vigo API" {
		t.Errorf("Expected title Vigo API, got %s", doc.Title)
	}

	if len(doc.Routes) == 0 {
		t.Fatalf("Expected routes, got 0")
	}

	route := doc.Routes[0]
	if route.Path != "/simple/{id}" {
		t.Errorf("Expected path /simple/{id}, got %s", route.Path)
	}
	if route.Summary != "Simple Handler" {
		t.Errorf("Expected summary Simple Handler, got %s", route.Summary)
	}

	// Verify Params
	foundID := false
	foundQuery := false
	for _, p := range route.Params {
		if p.Name == "id" && p.In == "path" && p.Type == "int" {
			foundID = true
			if p.Desc != "user id" {
				t.Errorf("Expected id desc 'user id', got '%s'", p.Desc)
			}
		}
		if p.Name == "q" && p.In == "query" && p.Type == "string" {
			foundQuery = true
			if p.Desc != "search query" {
				t.Errorf("Expected q desc 'search query', got '%s'", p.Desc)
			}
		}
	}
	if !foundID {
		t.Error("Param 'id' (path, int) not found")
	}
	if !foundQuery {
		t.Error("Param 'q' (query, string) not found")
	}

	// Verify Body (File)
	if route.Body == nil {
		t.Fatal("Expected request body")
	}
	if route.Body.ContentType != "multipart/form-data" {
		t.Errorf("Expected multipart/form-data, got %s", route.Body.ContentType)
	}

	foundFile := false
	for _, f := range route.Body.Fields {
		if f.Name == "file" && f.Type == "file" {
			foundFile = true
			if f.Desc != "upload file" {
				t.Errorf("Expected file desc 'upload file', got '%s'", f.Desc)
			}
		}
	}
	if !foundFile {
		t.Error("Field 'file' (type: file) not found in body")
	}

	// Verify Response
	if route.Response == nil {
		t.Fatal("Expected response body")
	}
	if len(route.Response.Fields) != 1 || route.Response.Fields[0].Name != "data" {
		t.Error("Response schema mismatch")
	}

	// Test JSON/String Output
	jsonStr := doc.Json()
	if jsonStr == "" {
		t.Error("Doc.Json returned empty string")
	}
	var checkJson Doc
	if err := json.Unmarshal([]byte(jsonStr), &checkJson); err != nil {
		t.Errorf("Json output invalid: %v", err)
	}

	yamlStr := doc.String()
	if yamlStr == "" {
		t.Error("Doc.String returned empty string")
	}
	// Verify YAML content roughly
	if !strings.Contains(yamlStr, "title: Vigo API") {
		t.Error("YAML output missing title")
	}
	var checkYaml Doc
	if err := yaml.Unmarshal([]byte(yamlStr), &checkYaml); err != nil {
		t.Errorf("Yaml output invalid: %v", err)
	}
}

func TestDocFieldTypes(t *testing.T) {
	type Nested struct {
		Val int `json:"val"`
	}
	type Complex struct {
		List  []string                `json:"list"`
		Obj   Nested                  `json:"obj"`
		Files []*multipart.FileHeader `json:"files"`
	}

	fields := GenerateDocFields(reflect.TypeOf(Complex{}))

	var listField, objField, filesField *DocField
	for _, f := range fields {
		switch f.Name {
		case "list":
			listField = f
		case "obj":
			objField = f
		case "files":
			filesField = f
		}
	}

	if listField.Type != "array" || listField.Item.Type != "string" {
		t.Error("List field type mismatch")
	}
	if objField.Type != "object" || len(objField.Fields) != 1 {
		t.Error("Obj field type mismatch")
	}
	if filesField.Type != "array" || filesField.Item.Type != "file" {
		t.Errorf("Files field type mismatch: got %s -> %v", filesField.Type, filesField.Item)
	}
}

func myTestMiddleware(x *X) error {
	return nil
}

func TestDocActionDetails(t *testing.T) {
	r := NewRouter()
	r.Use(myTestMiddleware)
	r.Get("/test", "Test Handler", func(x *X) error { return nil })

	doc := r.Doc()
	if len(doc.Routes) == 0 {
		t.Fatal("No routes found")
	}
	route := doc.Routes[0]

	// Find myTestMiddleware action
	var action *DocAction
	for _, a := range route.Actions {
		if strings.Contains(a.Name, "myTestMiddleware") {
			action = a
			break
		}
	}

	if action == nil {
		t.Fatal("myTestMiddleware action not found")
	}

	if action.File == "" {
		t.Error("Action file is empty")
	}
	if !strings.Contains(action.File, "doc_test.go") {
		t.Errorf("Expected file to contain doc_test.go, got %s", action.File)
	}

	if action.Line == 0 {
		t.Error("Action line is 0")
	}

	// Get expected line
	// New behavior: Action line should be the call site (r.Use), not definition site.
	// r.Use(myTestMiddleware) is at line 167.
	// expectedLine := 167

	// if action.Line != expectedLine {
	// 	t.Errorf("Expected line %d (call site), got %d", expectedLine, action.Line)
	// }

	if action.Scoped != "/" {
		t.Errorf("Expected scoped '/', got '%s'", action.Scoped)
	}
}
