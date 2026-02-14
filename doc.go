package vigo

import (
	_ "embed"
	"encoding/json"
	"mime/multipart"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultApiDesc = `API documentation
Query parameters for Api JSON Doc endpoint:
  - prefix=/user&method=GET  Filter by path prefix and method
  - query=list               Fuzzy search by path
  - mode=full                Show full details (params, body, response)
  - mode=debug               Show debug info (handlers, file location)`

// Doc defines a concise API documentation structure
type Doc struct {
	Title   string             `json:"title" yaml:"title"`
	Version string             `json:"version" yaml:"version"`
	Desc    string             `json:"desc,omitempty" yaml:"desc,omitempty"`
	Routes  []*DocRoute        `json:"routes" yaml:"routes"`
	Others  map[uint]*DocRoute `json:"others,omitempty" yaml:"others,omitempty"`
}

// String returns the YAML representation of the documentation
func (d *Doc) String() string {
	bs, _ := yaml.Marshal(d)
	return string(bs)
}

// Json returns the compact JSON representation of the documentation
func (d *Doc) Json() string {
	bs, _ := json.Marshal(d) // compact by default
	return string(bs)
}

// DocRoute defines a single route endpoint
type DocRoute struct {
	Method   string             `json:"method" yaml:"method"`
	Path     string             `json:"path" yaml:"path"`
	Summary  string             `json:"summary" yaml:"summary"`
	Params   []*DocParam        `json:"params,omitempty" yaml:"params,omitempty"`
	Body     *DocBody           `json:"body,omitempty" yaml:"body,omitempty"`
	Actions  []*DocAction       `json:"actions,omitempty" yaml:"actions,omitempty"`
	Response *DocBody           `json:"response,omitempty" yaml:"response,omitempty"`
	Others   map[uint]*DocRoute `json:"others,omitempty" yaml:"others,omitempty"`
}

type DocAction struct {
	Name   string `json:"name" yaml:"name"`
	File   string `json:"file" yaml:"file"`
	Line   int    `json:"line" yaml:"line"`
	Scoped string `json:"scoped,omitempty" yaml:"scoped,omitempty"`
}

type DocParam struct {
	Name     string      `json:"name" yaml:"name"`
	In       string      `json:"in" yaml:"in"` // path, query, header
	Type     string      `json:"type" yaml:"type"`
	Required bool        `json:"required" yaml:"required"`
	Desc     string      `json:"desc,omitempty" yaml:"desc,omitempty"`
	Default  interface{} `json:"default,omitempty" yaml:"default,omitempty"`
}

type DocBody struct {
	ContentType string      `json:"content_type" yaml:"content_type"`         // application/json, multipart/form-data
	Type        string      `json:"type" yaml:"type"`                         // string, int, bool, number, object, array
	Item        *DocField   `json:"item,omitempty" yaml:"item,omitempty"`     // For arrays
	Fields      []*DocField `json:"fields,omitempty" yaml:"fields,omitempty"` // For objects
}

type DocField struct {
	Name     string      `json:"name" yaml:"name"`
	Type     string      `json:"type" yaml:"type"` // string, int, bool, number, object, array, file
	Required bool        `json:"required" yaml:"required"`
	Desc     string      `json:"desc,omitempty" yaml:"desc,omitempty"`
	Default  interface{} `json:"default,omitempty" yaml:"default,omitempty"` // for string, int, bool, number
	Item     *DocField   `json:"item,omitempty" yaml:"item,omitempty"`       // For arrays
	Fields   []*DocField `json:"fields,omitempty" yaml:"fields,omitempty"`   // For objects
}

// Implementation of Router interface methods
type DocJsonRequest struct {
	Prefix *string `src:"query" json:"prefix" desc:"strict prefix match"`
	Query  *string `src:"query" json:"query" desc:"fuzzy path match"`
	Method *string `src:"query" json:"method" desc:"filter by method"`
	Mode   *string `src:"query" json:"mode" desc:"mode: simple(default)|full|debug"`
}

func (app *Application) EnableApiDoc() {
	if app.config.DocPath != "" {
		_ = app.Router().Get(app.config.DocPath+".json", "get server api documentation", func(x *X, arg *DocJsonRequest) error {
			res := app.Router().Doc()

			// Filter
			if arg.Prefix != nil || arg.Method != nil || arg.Query != nil {
				filtered := make([]*DocRoute, 0, len(res.Routes))
				for _, r := range res.Routes {
					if arg.Prefix != nil && !strings.HasPrefix(r.Path, *arg.Prefix) {
						continue
					}
					if arg.Query != nil && !strings.Contains(r.Path, *arg.Query) {
						continue
					}
					if arg.Method != nil && !strings.EqualFold(r.Method, *arg.Method) {
						continue
					}
					filtered = append(filtered, r)
				}
				res.Routes = filtered
			}

			// Mode
			mode := "simple"
			if arg.Mode != nil {
				mode = *arg.Mode
			}
			for _, r := range res.Routes {
				if mode != "debug" {
					r.Actions = nil
				}
				if mode == "simple" {
					r.Params = nil
					r.Body = nil
					r.Response = nil
				}
			}

			x.Stop()
			return x.JSON(res)
		})
		_ = app.Router().Get(app.config.DocPath, func(x *X) error {
			x.Stop()
			jsonPath := "./" + filepath.Base(app.config.DocPath) + ".json"
			return x.HTMLTemplate(docTemplate, jsonPath)
		})
	}
}

//go:embed doc.html
var docTemplate string

func (r *route) Doc() *Doc {
	doc := &Doc{
		Title:   "Vigo API",
		Version: "1.0.0",
		Desc:    DefaultApiDesc,
		Routes:  make([]*DocRoute, 0),
	}

	var traverse func(node *route, prefix string)
	traverse = func(node *route, prefix string) {
		currentPath := prefix
		if node.fragment != "" {
			currentPath += "/" + node.fragment
		}
		// Clean up double slashes just in case
		currentPath = strings.ReplaceAll(currentPath, "//", "/")

		if len(node.methods) > 0 {
			// Process methods
			for method, mh := range node.methods {
				if mh.Desc == "" {
					continue
				}
				route := &DocRoute{
					Method:  method,
					Path:    currentPath,
					Summary: mh.Desc,
				}

				if handlersInfo, ok := node.handlersInfoCache[method]; ok {
					for _, info := range handlersInfo {
						if info == nil {
							continue
						}
						val := reflect.ValueOf(info.Func)
						name := ""
						if val.Kind() == reflect.Func {
							op := val.Pointer()
							fn := runtime.FuncForPC(op)
							if fn != nil {
								name = fn.Name()
							}
						}

						route.Actions = append(route.Actions, &DocAction{
							Name:   name,
							File:   info.File,
							Line:   info.Line,
							Scoped: info.Scoped,
						})
					}
				} else if handlers, ok := node.handlersCache[method]; ok {
					for _, h := range handlers {
						if _, ok := h.(string); ok {
							continue
						}
						val := reflect.ValueOf(h)
						if val.Kind() == reflect.Func {
							op := val.Pointer()
							fn := runtime.FuncForPC(op)
							if fn != nil {
								file, line := fn.FileLine(op)
								route.Actions = append(route.Actions, &DocAction{
									Name: fn.Name(),
									File: file,
									Line: line,
								})
							}
						}
					}
				}

				// Parse Request
				if mh.Args != nil {
					// Check if it's already reflect.Type
					if t, ok := mh.Args.(reflect.Type); ok {
						route.Params, route.Body = parseDocArgs(t)
					} else {
						// Assume it's an instance or struct
						route.Params, route.Body = parseDocArgs(reflect.TypeOf(mh.Args))
					}
				}

				// Parse Response (Only 200 OK)
				if mh.Response != nil {
					if t, ok := mh.Response.(reflect.Type); ok {
						route.Response = parseDocResponse(t)
					} else {
						route.Response = parseDocResponse(reflect.TypeOf(mh.Response))
					}
				}

				doc.Routes = append(doc.Routes, route)
			}
		}

		// Traverse children
		for _, child := range node.children {
			traverse(child, currentPath)
		}
	}

	traverse(r, "")
	return doc
}

// Helpers

func parseDocArgs(t reflect.Type) ([]*DocParam, *DocBody) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil
	}

	var params []*DocParam
	var body *DocBody

	// Helper to process fields recursively for embedded structs
	var processFields func(t reflect.Type)
	processFields = func(t reflect.Type) {
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return
		}

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("src")
			jsonTag := field.Tag.Get("json")
			desc := field.Tag.Get("desc")
			name := strings.Split(jsonTag, ",")[0]

			// Handle embedded struct (Anonymous)
			if field.Anonymous && (jsonTag == "" || jsonTag == ",") {
				processFields(field.Type)
				continue
			}

			if name == "" {
				name = field.Name
			}
			if name == "-" {
				continue
			}

			parts := strings.Split(tag, "@")
			source := parts[0]
			if source == "" {
				source = "json" // Default
			}

			switch source {
			case "path", "query", "header":
				defaultVal := field.Tag.Get("default")
				required := field.Type.Kind() != reflect.Ptr // Pointer = Optional
				if defaultVal != "" {
					required = false
				}

				p := &DocParam{
					Name:     name,
					In:       source,
					Type:     getDocType(field.Type),
					Required: required,
					Desc:     desc,
				}
				if defaultVal != "" {
					p.Default = defaultVal
				}

				if source == "path" && len(parts) > 1 {
					p.Name = parts[1] // Use alias for path param
				}
				params = append(params, p)

			case "json", "form":
				if body == nil {
					body = &DocBody{
						Type:   "object",
						Fields: make([]*DocField, 0),
					}
					if source == "form" {
						body.ContentType = "multipart/form-data"
					} else {
						body.ContentType = "application/json"
					}
				}
				if source == "form" {
					body.ContentType = "multipart/form-data"
				}

				defaultStr := field.Tag.Get("default")
				var def interface{}
				if defaultStr != "" {
					def = defaultStr
				}
				body.Fields = append(body.Fields, generateDocField(field.Type, name, desc, def, nil))
			}
		}
	}

	processFields(t)
	return params, body
}

func parseDocResponse(t reflect.Type) *DocBody {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	docType := getDocType(t)
	body := &DocBody{
		ContentType: "application/json",
		Type:        docType,
	}

	if docType == "array" {
		body.Item = generateDocField(t.Elem(), "", "", nil, nil)
	} else if docType == "object" {
		body.Fields = GenerateDocFields(t)
	}

	return body
}

func generateDocField(t reflect.Type, name string, desc string, defaultVal interface{}, visited map[reflect.Type]bool) *DocField {
	f := &DocField{
		Name:     name,
		Type:     getDocType(t),
		Required: true, // Default to required unless ptr?
		Desc:     desc,
		Default:  defaultVal,
	}

	if defaultVal != nil {
		f.Required = false
	}

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		f.Required = false
	}

	// Handle File
	if f.Type == "file" {
		return f
	}

	// Check for circular reference
	if visited == nil {
		visited = make(map[reflect.Type]bool)
	}
	if visited[t] {
		return f
	}
	visited[t] = true

	if f.Type == "array" {
		f.Item = generateDocField(t.Elem(), "", "", nil, visited)
	} else if f.Type == "object" {
		f.Fields = generateDocFieldsWithVisited(t, visited)
	}

	return f
}

func GenerateDocFields(t reflect.Type) []*DocField {
	return generateDocFieldsWithVisited(t, make(map[reflect.Type]bool))
}

func generateDocFieldsWithVisited(t reflect.Type, visited map[reflect.Type]bool) []*DocField {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	// Check for circular reference
	if visited[t] {
		return nil
	}
	visited[t] = true

	var fields []*DocField
	// Helper to process fields recursively for embedded structs
	var processFields func(t reflect.Type)
	processFields = func(t reflect.Type) {
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return
		}

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			// ignore private fields
			if field.PkgPath != "" {
				continue
			}
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}
			name := strings.Split(jsonTag, ",")[0]

			// Handle embedded struct (Anonymous)
			if field.Anonymous && (jsonTag == "" || jsonTag == ",") {
				processFields(field.Type)
				continue
			}

			if name == "" {
				name = field.Name
			}

			defaultStr := field.Tag.Get("default")
			var def interface{}
			if defaultStr != "" {
				def = defaultStr
			}
			fields = append(fields, generateDocField(field.Type, name, field.Tag.Get("desc"), def, visited))
		}
	}
	processFields(t)
	return fields
}

func getDocType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Handle multipart.FileHeader
	if t == reflect.TypeOf(multipart.FileHeader{}) {
		return "file"
	}
	// Handle slice of files
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Ptr && t.Elem().Elem() == reflect.TypeOf(multipart.FileHeader{}) {
		return "array" // Item will be file
	}
	if t.Kind() == reflect.Slice && t.Elem() == reflect.TypeOf(multipart.FileHeader{}) {
		return "array"
	}

	switch t.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		if t.String() == "time.Time" {
			return "string"
		}
		return "object"
	default:
		return "string"
	}
}
