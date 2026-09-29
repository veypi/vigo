package flags

import (
	"encoding"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigIssue describes an ignored configuration value without exposing its contents.
// Configuration issues are diagnostic only; LoadCfg and Parse do not log them.
type ConfigIssue struct {
	Source string
	Field  string
	Reason string
}

// SetDefaults fills unset fields from default tags. Existing nonzero values are
// application defaults. Call before applying file/env/CLI values, never after.
func SetDefaults(cfg any) []ConfigIssue {
	var issues []ConfigIssue
	applyDefaults(reflect.ValueOf(cfg), "", make(map[any]bool), &issues)
	return issues
}

func applyDefaults(v reflect.Value, name string, seen map[any]bool, issues *[]ConfigIssue) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if !v.CanSet() || !configStruct(v.Type()) || seen[v.Type().Elem()] {
				return
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		if seen[v.Interface()] {
			return
		}
		seen[v.Interface()] = true
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct || !configStruct(v.Type()) {
		return
	}
	if seen[v.Type()] {
		return
	}
	seen[v.Type()] = true
	defer delete(seen, v.Type())
	for i := 0; i < v.NumField(); i++ {
		field, meta := v.Field(i), v.Type().Field(i)
		if !field.CanSet() {
			continue
		}
		key := strings.Split(meta.Tag.Get("json"), ",")[0]
		if key == "-" {
			continue
		}
		if key == "" {
			key = meta.Name
		}
		path := buildFlagName(name, key)
		if configStruct(field.Type()) {
			// Stop recursive pointer types; configuration schemas must be finite.
			if field.Kind() == reflect.Pointer && field.Type().Elem() == v.Type() && field.IsNil() {
				continue
			}
			applyDefaults(field, path, seen, issues)
			continue
		}
		address := field.Addr().Interface()
		if seen[address] {
			continue
		}
		seen[address] = true
		if tag, ok := meta.Tag.Lookup("default"); ok && field.IsZero() {
			value, err := parseValue(field.Type(), tag)
			if err != nil {
				*issues = append(*issues, ConfigIssue{"default", path, "invalid default value"})
			} else {
				field.Set(value)
			}
		}
	}
}

// LoadCfg applies defaults and then an optional configuration file. JSON uses
// json tags; YAML uses yaml tags. Unknown fields are ignored, invalid fields
// retain their fallback, and unreadable/malformed files leave defaults usable.
// Nested structs merge by field; collections and custom decoders apply atomically.
// This function never reads environment variables, logs errors, or writes files.
func LoadCfg(path string, cfg any) []ConfigIssue {
	issues := SetDefaults(cfg)
	return append(issues, loadConfigFile(path, cfg)...)
}

func loadConfigFile(path string, cfg any) []ConfigIssue {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []ConfigIssue{{path, "", "cannot read configuration file"}}
	}
	var node configNode
	if strings.EqualFold(filepath.Ext(path), ".json") {
		if !json.Valid(data) {
			return []ConfigIssue{{path, "", "invalid JSON document"}}
		}
		node = configNode{raw: data}
	} else {
		var doc yaml.Node
		if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) != 1 {
			return []ConfigIssue{{path, "", "invalid YAML document"}}
		}
		node = configNode{yaml: doc.Content[0]}
	}
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return []ConfigIssue{{path, "", "configuration target must be a non-nil pointer"}}
	}
	var issues []ConfigIssue
	applyConfig(node, v.Elem(), "", path, &issues, 0)
	return issues
}

type configNode struct {
	raw    json.RawMessage
	yaml   *yaml.Node
	quoted bool
}

func (n configNode) decode(dst any) error {
	if n.yaml != nil {
		return n.yaml.Decode(dst)
	}
	if n.quoted && strings.TrimSpace(string(n.raw)) != "null" {
		var text string
		if err := json.Unmarshal(n.raw, &text); err != nil {
			return err
		}
		if !json.Valid([]byte(text)) {
			return fmt.Errorf("invalid quoted JSON value")
		}
		return json.Unmarshal([]byte(text), dst)
	}
	return json.Unmarshal(n.raw, dst)
}

func (n configNode) object() (map[string]configNode, bool) {
	out := make(map[string]configNode)
	if n.yaml != nil {
		var values map[string]yaml.Node
		if n.yaml.Decode(&values) != nil || values == nil {
			return nil, false
		}
		for key, value := range values {
			out[key] = configNode{yaml: &value}
		}
	} else {
		var values map[string]json.RawMessage
		if json.Unmarshal(n.raw, &values) != nil || values == nil {
			return nil, false
		}
		for key, value := range values {
			out[key] = configNode{raw: value}
		}
	}
	return out, true
}

func applyConfig(node configNode, dst reflect.Value, name, source string, issues *[]ConfigIssue, depth int) {
	if !dst.CanSet() {
		return
	}
	if depth > 64 {
		*issues = append(*issues, ConfigIssue{source, name, "configuration nesting is too deep"})
		return
	}
	if configStruct(dst.Type()) {
		entries, ok := node.object()
		if !ok {
			*issues = append(*issues, ConfigIssue{source, name, "expected a configuration object"})
			return
		}
		if dst.Kind() == reflect.Pointer {
			if dst.IsNil() {
				dst.Set(reflect.New(dst.Type().Elem()))
			}
			dst = dst.Elem()
		}
		fields, inlineMap := configFields(dst.Type(), node.yaml != nil, nil, make(map[reflect.Type]bool))
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			index, found := fields[key]
			if !found && node.yaml == nil {
				// encoding/json also accepts case-insensitive field names.
				for candidate, candidateIndex := range fields {
					if strings.EqualFold(candidate, key) {
						index, found = candidateIndex, true
						break
					}
				}
			}
			if !found || index == nil {
				if !found && inlineMap != nil {
					target := fieldAt(dst, inlineMap, true)
					value := reflect.New(target.Type().Elem())
					if entries[key].decode(value.Interface()) != nil {
						*issues = append(*issues, ConfigIssue{source, buildFlagName(name, key), "invalid field value"})
						continue
					}
					if target.IsNil() {
						target.Set(reflect.MakeMap(target.Type()))
					}
					target.SetMapIndex(reflect.ValueOf(key).Convert(target.Type().Key()), value.Elem())
				}
				continue
			}
			target := fieldAt(dst, index, true)
			entry := entries[key]
			if node.yaml == nil {
				typ := dst.Type()
				var meta reflect.StructField
				for _, part := range index {
					if typ.Kind() == reflect.Pointer {
						typ = typ.Elem()
					}
					meta = typ.Field(part)
					typ = meta.Type
				}
				if typ.Kind() == reflect.Pointer {
					typ = typ.Elem()
				}
				switch typ.Kind() {
				case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
					for _, option := range strings.Split(meta.Tag.Get("json"), ",")[1:] {
						if option == "string" {
							entry.quoted = true
						}
					}
				}
			}
			applyConfig(entry, target, buildFlagName(name, key), source, issues, depth+1)
		}
		return
	}
	value := reflect.New(dst.Type())
	if node.decode(value.Interface()) != nil {
		*issues = append(*issues, ConfigIssue{source, name, "invalid field value"})
		return
	}
	// null for non-nullable scalar values is absence, not an explicit zero.
	if value.Elem().IsZero() && ((node.yaml != nil && node.yaml.Tag == "!!null") || (node.yaml == nil && strings.TrimSpace(string(node.raw)) == "null")) {
		return
	}
	dst.Set(value.Elem())
}

func configStruct(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	p := reflect.PointerTo(t)
	return !p.Implements(reflect.TypeFor[json.Unmarshaler]()) &&
		!p.Implements(reflect.TypeFor[yaml.Unmarshaler]()) &&
		!p.Implements(reflect.TypeFor[encoding.TextUnmarshaler]())
}

func configFields(t reflect.Type, yamlFormat bool, prefix []int, visiting map[reflect.Type]bool) (map[string][]int, []int) {
	out := make(map[string][]int)
	var inlineMap []int
	if visiting[t] {
		return out, nil
	}
	visiting[t] = true
	defer delete(visiting, t)
	for i := 0; i < t.NumField(); i++ {
		meta := t.Field(i)
		if !meta.IsExported() {
			continue
		}
		tagKey := "json"
		if yamlFormat {
			tagKey = "yaml"
		}
		tag := strings.Split(meta.Tag.Get(tagKey), ",")
		if tag[0] == "-" {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		inline := !yamlFormat && meta.Anonymous && tag[0] == ""
		if yamlFormat {
			for _, option := range tag[1:] {
				if option == "inline" {
					inline = true
				}
			}
		}
		if inline && meta.Type.Kind() == reflect.Map && meta.Type.Key().Kind() == reflect.String {
			inlineMap = index
			continue
		}
		if inline && configStruct(meta.Type) {
			child := meta.Type
			if child.Kind() == reflect.Pointer {
				child = child.Elem()
			}
			children, childMap := configFields(child, yamlFormat, index, visiting)
			if childMap != nil {
				inlineMap = childMap
			}
			for key, path := range children {
				if previous, ok := out[key]; !ok || len(path) < len(previous) {
					out[key] = path
				} else if len(path) == len(previous) {
					out[key] = nil
				}
			}
			continue
		}
		key := tag[0]
		if key == "" {
			key = meta.Name
			if yamlFormat {
				key = strings.ToLower(key)
			}
		}
		out[key] = index
	}
	return out, inlineMap
}

// fieldAt preserves existing pointer identity (other modules may retain it).
// Registration uses create=false so it does not allocate or modify configuration.
func fieldAt(v reflect.Value, path []int, create bool) reflect.Value {
	for _, index := range path {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !create || !v.CanSet() {
					return reflect.Value{}
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(index)
	}
	return v
}
