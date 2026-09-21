package flags

import (
	"encoding"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func LoadEnvOr(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

func buildEnvKey(prefix, name string) string {
	if prefix == "" {
		return strings.ToUpper(name)
	}
	return prefix + "_" + strings.ToUpper(name)
}

func buildFlagName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// DurationValue 自定义 Duration 类型的命令行参数
type DurationValue time.Duration

func (d *DurationValue) String() string {
	return (*time.Duration)(d).String()
}

func (d *DurationValue) Set(s string) error {
	duration, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = DurationValue(duration)
	return nil
}

// TimeValue 自定义 Time 类型的命令行参数
type TimeValue time.Time

func (t *TimeValue) String() string {
	return (*time.Time)(t).Format(time.RFC3339)
}

func (t *TimeValue) Set(s string) error {
	parsedTime, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// 尝试其他格式
		if parsedTime, err = time.Parse("2006-01-02 15:04:05", s); err != nil {
			if parsedTime, err = time.Parse("2006-01-02", s); err != nil {
				return err
			}
		}
	}
	*t = TimeValue(parsedTime)
	return nil
}

// FileValue 从文件加载复杂类型数据的自定义类型
type FileValue struct {
	target   reflect.Value
	typeName string
}

func NewFileValue(target reflect.Value) *FileValue {
	return &FileValue{
		target:   target,
		typeName: target.Type().String(),
	}
}

func (f *FileValue) String() string {
	if f.target.IsValid() {
		if data, err := json.Marshal(f.target.Interface()); err == nil {
			return string(data)
		}
	}
	return "{}"
}

func (f *FileValue) Set(filePath string) error {
	// 读取文件内容
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %v", filePath, err)
	}

	// 创建目标类型的新实例
	newValue := reflect.New(f.target.Type()).Interface()

	// 解析JSON到目标类型
	if err := json.Unmarshal(data, newValue); err != nil {
		return fmt.Errorf("failed to parse JSON from file %s: %v", filePath, err)
	}

	// 设置值
	f.target.Set(reflect.ValueOf(newValue).Elem())
	return nil
}

// AutoRegister declares flag/env bindings without changing cfg. Defaults, files,
// environment variables and explicit flags are applied together by Parse.
func (f *Flags) AutoRegister(cfg any) {
	root := reflect.ValueOf(cfg)
	if root.Kind() != reflect.Pointer || root.IsNil() || root.Elem().Kind() != reflect.Struct {
		f.registrationErr = fmt.Errorf("configuration must be a non-nil pointer to a struct")
		return
	}
	f.configs = append(f.configs, cfg)
	f.registerFields(root, root.Elem().Type(), nil, "", "", make(map[reflect.Type]bool))
}

func (f *Flags) registerFields(root reflect.Value, typ reflect.Type, prefix []int, envPrefix, flagPrefix string, visiting map[reflect.Type]bool) {
	if visiting[typ] {
		return
	}
	visiting[typ] = true
	defer delete(visiting, typ)
	for i := 0; i < typ.NumField(); i++ {
		meta := typ.Field(i)
		if !meta.IsExported() {
			continue
		}
		tag := strings.Split(meta.Tag.Get("json"), ",")[0]
		if tag == "-" {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		if meta.Anonymous && configStruct(meta.Type) && tag == "" {
			nested := meta.Type
			if nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			f.registerFields(root, nested, index, envPrefix, flagPrefix, visiting)
			continue
		}
		if tag == "" {
			continue
		}
		envKey, flagName := buildEnvKey(envPrefix, tag), buildFlagName(flagPrefix, tag)
		if configStruct(meta.Type) {
			nested := meta.Type
			if nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			f.registerFields(root, nested, index, envKey, flagName, visiting)
			continue
		}
		value := &boundValue{root: root, path: index, typ: meta.Type, env: envKey, name: flagName, owner: f}
		f.bindings = append(f.bindings, value)
		usage := meta.Tag.Get("desc")
		if usage == "" {
			usage = "set " + flagName + " value"
		}
		usage += " (env: " + envKey + ")"
		f.Var(value, flagName, usage)
		if current := value.field(false); !current.IsValid() || current.IsZero() {
			if fallback, ok := meta.Tag.Lookup("default"); ok {
				f.Lookup(flagName).DefValue = fallback
			}
		}
		if short := meta.Tag.Get("short"); short != "" && short != "h" {
			f.Var(value, short, usage)
			f.Lookup(short).DefValue = f.Lookup(flagName).DefValue
		}
	}
}

type boundValue struct {
	root      reflect.Value
	path      []int
	typ       reflect.Type
	env, name string
	owner     *Flags
}

func (v *boundValue) field(create bool) reflect.Value { return fieldAt(v.root, v.path, create) }
func (v *boundValue) String() string {
	// flag.PrintDefaults calls String on a zero value of the flag.Value type.
	if v == nil || !v.root.IsValid() {
		return ""
	}
	field := v.field(false)
	if !field.IsValid() {
		field = reflect.Zero(v.typ)
	}
	if field.Kind() == reflect.String {
		return field.String()
	}
	if field.CanInterface() {
		if s, ok := field.Interface().(fmt.Stringer); ok {
			return s.String()
		}
		if b, err := json.Marshal(field.Interface()); err == nil {
			return string(b)
		}
	}
	return ""
}
func (v *boundValue) IsBoolFlag() bool { return v.typ.Kind() == reflect.Bool }
func (v *boundValue) Set(s string) error {
	value, err := parseValue(v.typ, s)
	if err != nil {
		return fmt.Errorf("invalid %s value", v.typ)
	}
	if v.owner.collecting {
		// Parse each argument once. Delay assignment until lower-priority layers are ready.
		v.owner.explicit = append(v.owner.explicit, func() { v.field(true).Set(value) })
	} else {
		v.field(true).Set(value)
	}
	return nil
}

type recordingValue struct {
	flag.Value
	target reflect.Value
	owner  *Flags
}

func (v *recordingValue) String() string {
	if v == nil || v.Value == nil {
		return ""
	}
	return v.Value.String()
}

func (v *recordingValue) IsBoolFlag() bool {
	value, ok := v.Value.(interface{ IsBoolFlag() bool })
	return ok && value.IsBoolFlag()
}

func (v *recordingValue) Get() any {
	if value, ok := v.Value.(flag.Getter); ok {
		return value.Get()
	}
	return v.Value.String()
}

func (v *recordingValue) Set(s string) error {
	if err := v.Value.Set(s); err != nil {
		return err
	}
	if v.owner.collecting {
		value := reflect.New(v.target.Type()).Elem()
		value.Set(v.target)
		v.owner.explicit = append(v.owner.explicit, func() { v.target.Set(value) })
	}
	return nil
}

// parseValue always decodes into an independent value, including slices/maps and
// custom decoders, so a failure cannot partially overwrite a fallback.
func parseValue(typ reflect.Type, raw string) (reflect.Value, error) {
	value := reflect.New(typ).Elem()
	if typ == reflect.TypeFor[time.Duration]() {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return value, err
		}
		value.SetInt(int64(d))
		return value, nil
	}
	if typ == reflect.TypeFor[time.Time]() {
		var result TimeValue
		if err := result.Set(raw); err != nil {
			return value, err
		}
		value.Set(reflect.ValueOf(time.Time(result)))
		return value, nil
	}
	if decoder, ok := value.Addr().Interface().(encoding.TextUnmarshaler); ok {
		return value, decoder.UnmarshalText([]byte(raw))
	}
	if typ.Kind() == reflect.Pointer {
		child, err := parseValue(typ.Elem(), raw)
		if err != nil {
			return value, err
		}
		value.Set(reflect.New(typ.Elem()))
		value.Elem().Set(child)
		return value, nil
	}
	switch typ.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return value, err
		}
		value.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 0, typ.Bits())
		if err != nil {
			return value, err
		}
		value.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 0, typ.Bits())
		if err != nil {
			return value, err
		}
		value.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, typ.Bits())
		if err != nil {
			return value, err
		}
		value.SetFloat(n)
	default:
		data := []byte(raw)
		if !json.Valid(data) {
			var err error
			data, err = os.ReadFile(raw)
			if err != nil {
				return value, err
			}
		}
		if err := json.Unmarshal(data, value.Addr().Interface()); err != nil {
			return value, err
		}
	}
	return value, nil
}
