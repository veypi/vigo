package flags

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type fileNested struct {
	Port  int    `json:"port" yaml:"port" default:"8080"`
	Label string `json:"label" yaml:"label" default:"nested"`
}
type fileOptions struct {
	Name    string         `json:"name" yaml:"name" default:"default-name"`
	Enabled bool           `json:"enabled" yaml:"enabled" default:"true"`
	Count   int            `json:"count" yaml:"count" default:"10"`
	Small   int8           `json:"small" yaml:"small" default:"12"`
	Names   []string       `json:"names" yaml:"names" default:"[\"default\"]"`
	Labels  map[string]int `json:"labels" yaml:"labels" default:"{\"default\":1}"`
	Nested  *fileNested    `json:"nested" yaml:"nested"`
	Hidden  string         `json:"-" yaml:"-"`
	private string
}

func writeConfig(t *testing.T, extension, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config"+extension)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCfgFieldFallback(t *testing.T) {
	for _, tc := range []struct{ ext, body string }{
		{".yaml", "name: device-key\nextra: ignored\nenabled: wrong\ncount: {}\nsmall: 999\nnames: [valid, {}]\nlabels: {good: 2, bad: text}\nnested: {port: bad, label: preserved}\nhidden: changed\nprivate: changed\n"},
		{".JSON", `{"name":"device-key","extra":true,"enabled":"wrong","count":{},"small":999,"names":["valid",{}],"labels":{"good":2,"bad":"text"},"nested":{"port":"bad","label":"preserved"},"hidden":"changed","private":"changed"}`},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			nested := &fileNested{Port: 9000}
			cfg := &fileOptions{Nested: nested, Hidden: "private-default", private: "untouched"}
			path := writeConfig(t, tc.ext, tc.body)
			issues := LoadCfg(path, cfg)
			if cfg.Name != "device-key" || !cfg.Enabled || cfg.Count != 10 || cfg.Small != 12 {
				t.Fatalf("fallback failed: %+v", cfg)
			}
			if !reflect.DeepEqual(cfg.Names, []string{"default"}) || !reflect.DeepEqual(cfg.Labels, map[string]int{"default": 1}) {
				t.Fatal("collection was partially applied")
			}
			if cfg.Nested != nested || nested.Port != 9000 || nested.Label != "preserved" {
				t.Fatal("nested fallback or pointer identity lost")
			}
			if cfg.Hidden != "private-default" || cfg.private != "untouched" {
				t.Fatal("ignored fields changed")
			}
			if len(issues) != 6 {
				t.Fatalf("expected 6 diagnostics, got %+v", issues)
			}
			for _, issue := range issues {
				if strings.Contains(issue.Reason, "device-key") {
					t.Fatal("diagnostic leaked a value")
				}
			}
			data, _ := os.ReadFile(path)
			if string(data) != tc.body {
				t.Fatal("load modified the config file")
			}
		})
	}
}

func TestLoadCfgExplicitZeroValues(t *testing.T) {
	for _, tc := range []struct{ ext, body string }{
		{".yaml", "name: ''\nenabled: false\ncount: 0\nnames: []\nlabels: {}\nnested: {port: 0, label: ''}\n"},
		{".json", `{"name":"","enabled":false,"count":0,"names":[],"labels":{},"nested":{"port":0,"label":""}}`},
	} {
		cfg := &fileOptions{}
		if issues := LoadCfg(writeConfig(t, tc.ext, tc.body), cfg); len(issues) != 0 {
			t.Fatal(issues)
		}
		if cfg.Name != "" || cfg.Enabled || cfg.Count != 0 || cfg.Names == nil || len(cfg.Names) != 0 || cfg.Labels == nil || len(cfg.Labels) != 0 || cfg.Nested.Port != 0 || cfg.Nested.Label != "" {
			t.Fatalf("explicit zero overwritten: %+v", cfg)
		}
	}
}

func TestLoadCfgBrokenFileAndMissingFile(t *testing.T) {
	for _, tc := range []struct{ ext, body string }{
		{".yaml", "name: applied?\n[broken"}, {".yaml", "[a, b]"}, {".yaml", "text"}, {".yaml", ""},
		{".json", `{"name":"applied?","count":`}, {".json", "[]"}, {".json", "null"},
	} {
		cfg := &fileOptions{}
		LoadCfg(writeConfig(t, tc.ext, tc.body), cfg)
		if cfg.Name != "default-name" || !cfg.Enabled || cfg.Count != 10 || cfg.Nested.Port != 8080 {
			t.Fatalf("broken file changed defaults: %+v", cfg)
		}
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		cfg := &fileOptions{}
		LoadCfg(path, cfg)
		if cfg.Name != "default-name" || !cfg.Enabled {
			t.Fatal("unreadable file lost defaults")
		}
	}
}

func TestLoadCfgDoesNotReadEnvironment(t *testing.T) {
	t.Setenv("NAME", "environment")
	cfg := &fileOptions{}
	LoadCfg(writeConfig(t, ".yaml", "name: file\n"), cfg)
	if cfg.Name != "file" {
		t.Fatal("file-only load applied environment")
	}
}

type EmbeddedFile struct {
	Value int `json:"value" yaml:"value" default:"7"`
}

func TestLoadCfgEmbeddedTagsAndAliases(t *testing.T) {
	type options struct {
		*EmbeddedFile `yaml:",inline"`
		Child         fileNested    `json:"child,omitempty" yaml:"child,omitempty"`
		Duration      time.Duration `json:"duration" yaml:"duration" default:"2s"`
		Time          time.Time     `json:"time" yaml:"time"`
	}
	for _, tc := range []struct{ ext, body string }{
		{".yaml", "value: 0\nbase: &base {port: 0, label: alias}\nchild: {<<: *base}\nduration: 3s\ntime: 2026-01-02T03:04:05Z\n"},
		{".json", `{"value":0,"child":{"port":0,"label":"alias"},"duration":3000000000,"time":"2026-01-02T03:04:05Z"}`},
	} {
		cfg := &options{}
		if issues := LoadCfg(writeConfig(t, tc.ext, tc.body), cfg); len(issues) != 0 {
			t.Fatal(issues)
		}
		if cfg.Value != 0 || cfg.Child.Port != 0 || cfg.Child.Label != "alias" || cfg.Duration != 3*time.Second || cfg.Time.Year() != 2026 {
			t.Fatalf("tags/aliases/custom types failed: %+v", cfg)
		}
	}
}

type atomicDecoder struct{ Value string }

func (v *atomicDecoder) UnmarshalJSON(data []byte) error {
	v.Value = "partial"
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v.Value = s
	return nil
}
func (v *atomicDecoder) UnmarshalYAML(node *yaml.Node) error {
	v.Value = "partial"
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v.Value = s
	return nil
}
func TestLoadCfgCustomDecoderFailureIsAtomic(t *testing.T) {
	for _, tc := range []struct{ ext, body string }{{".json", `{"custom":[]}`}, {".yaml", "custom: []"}} {
		cfg := &struct {
			Custom atomicDecoder `json:"custom" yaml:"custom"`
		}{atomicDecoder{"original"}}
		LoadCfg(writeConfig(t, tc.ext, tc.body), cfg)
		if cfg.Custom.Value != "original" {
			t.Fatal("failed custom decoder mutated original value")
		}
	}
}

func TestLoadCfgJSONQuotedFieldsAndYAMLInlineMap(t *testing.T) {
	quoted := &struct {
		Number  int    `json:"number,string" default:"8"`
		Text    string `json:"text,string"`
		Invalid int    `json:"invalid,string" default:"9"`
	}{}
	LoadCfg(writeConfig(t, ".json", `{"number":"0","text":"\"quoted\"","invalid":"wrong"}`), quoted)
	if quoted.Number != 0 || quoted.Text != "quoted" || quoted.Invalid != 9 {
		t.Fatalf("quoted JSON fields: %+v", quoted)
	}
	inline := &struct {
		Name  string         `yaml:"name"`
		Extra map[string]int `yaml:",inline"`
	}{Extra: map[string]int{"old": 1, "bad": 7}}
	LoadCfg(writeConfig(t, ".yaml", "name: inline\nnew: 2\nbad: wrong\n"), inline)
	if inline.Name != "inline" || !reflect.DeepEqual(inline.Extra, map[string]int{"old": 1, "bad": 7, "new": 2}) {
		t.Fatalf("YAML inline map: %+v", inline)
	}
}
