//
// register.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package flags

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"encoding/json"
	"github.com/joho/godotenv"
	"github.com/veypi/vigo/logv"
)

// isZeroValue 检查值是否为零值
func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.Struct:
		// 对于结构体，检查所有字段是否都是零值
		for i := 0; i < v.NumField(); i++ {
			if !isZeroValue(v.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		return v.Len() == 0
	case reflect.Map:
		return v.Len() == 0
	default:
		// 检查是否是 time.Duration 类型
		if v.Type() == reflect.TypeOf(time.Duration(0)) {
			return v.Int() == 0
		}
		// 对于其他复杂类型，认为不是零值
		return false
	}
}

func LoadEnvOr(key, defaultValue string) string {
	v := os.Getenv(key)
	if v != "" {
		return v
	}
	return defaultValue
}

// getDefaultValue 获取字段的默认值（优先使用环境变量，其次使用字段当前值，最后使用default标签）
func getDefaultValue(field reflect.Value, envKey, defaultTag string) string {
	// 优先使用环境变量
	if envValue := os.Getenv(envKey); envValue != "" {
		return envValue
	}

	// 如果字段值不是零值，则使用字段的当前值作为默认值
	if !isZeroValue(field) {
		switch field.Kind() {
		case reflect.String:
			return field.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			// 检查是否是 time.Duration 类型
			if field.Type() == reflect.TypeOf(time.Duration(0)) {
				duration := time.Duration(field.Int())
				return duration.String()
			}
			return strconv.FormatInt(field.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return strconv.FormatUint(field.Uint(), 10)
		case reflect.Bool:
			return strconv.FormatBool(field.Bool())
		case reflect.Float32, reflect.Float64:
			return strconv.FormatFloat(field.Float(), 'g', -1, 64)
		case reflect.Slice, reflect.Array, reflect.Map:
			// 对于复杂类型，返回JSON字符串
			if data, err := json.Marshal(field.Interface()); err == nil {
				return string(data)
			}
		default:
			// 检查是否是 time.Time 类型
			if field.Type() == reflect.TypeOf(time.Time{}) {
				t := field.Interface().(time.Time)
				return t.Format(time.RFC3339)
			}
		}
	}

	// 否则使用default标签的值
	return defaultTag
}

// getFieldDescription 获取字段的描述信息
func getFieldDescription(fieldType reflect.StructField, flagName, envKey string) string {

	// 其次使用 usage 标签
	if usage := fieldType.Tag.Get("usage"); usage != "" {
		return fmt.Sprintf("%s (env: %s)", usage, envKey)
	}

	// 最后使用默认描述
	return fmt.Sprintf("set %s value (env: %s)", flagName, envKey)
}

// buildEnvKey 构建环境变量键名，支持嵌套结构体
func buildEnvKey(prefix, fieldName string) string {
	if prefix == "" {
		return strings.ToUpper(fieldName)
	}
	return fmt.Sprintf("%s_%s", prefix, strings.ToUpper(fieldName))
}

// buildFlagName 构建命令行参数名，支持嵌套结构体
func buildFlagName(prefix, fieldName string) string {
	if prefix == "" {
		return fieldName
	}
	return fmt.Sprintf("%s.%s", prefix, fieldName)
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

// AutoRegister 自动注册命令行参数，支持嵌套结构体
func (fs *Flags) AutoRegister(config any) {
	godotenv.Load()
	fs.autoRegisterWithPrefix(config, "", "")
}

func (fs *Flags) registerValue(field reflect.Value, flagName, defaultValue, usage string) {
	// 根据字段类型注册不同的参数类型
	switch {
	case field.Type() == reflect.TypeOf(time.Duration(0)):
		// 处理 time.Duration 类型
		defaultDuration := time.Duration(0)
		if defaultValue != "" {
			if parsed, err := time.ParseDuration(defaultValue); err == nil {
				defaultDuration = parsed
			}
		}
		durationPtr := (*DurationValue)(field.Addr().Interface().(*time.Duration))
		fs.Var(durationPtr, flagName, usage)
		// 设置默认值
		*durationPtr = DurationValue(defaultDuration)

	case field.Type() == reflect.TypeOf(time.Time{}):
		// 处理 time.Time 类型
		defaultTime := time.Time{}
		if defaultValue != "" {
			if parsed, err := time.Parse(time.RFC3339, defaultValue); err == nil {
				defaultTime = parsed
			}
		}
		timePtr := (*TimeValue)(field.Addr().Interface().(*time.Time))
		fs.Var(timePtr, flagName, usage)
		// 设置默认值
		*timePtr = TimeValue(defaultTime)

	case field.Kind() == reflect.Slice || field.Kind() == reflect.Array:
		// 处理 slice 和 array 类型，从文件加载
		fileValue := NewFileValue(field)
		usage += " (file path to JSON array)"
		fs.Var(fileValue, flagName, usage)

		// 如果有默认值，尝试从文件加载或解析JSON
		if defaultValue != "" {
			if err := loadDefaultComplexValue(field, defaultValue); err != nil {
				fmt.Printf("Warning: failed to load default value for %s: %v\n", flagName, err)
			}
		}

	case field.Kind() == reflect.Map:
		// 处理 map 类型，从文件加载
		fileValue := NewFileValue(field)
		usage += " (file path to JSON object)"
		fs.Var(fileValue, flagName, usage)

		// 如果有默认值，尝试从文件加载或解析JSON
		if defaultValue != "" {
			if err := loadDefaultComplexValue(field, defaultValue); err != nil {
				fmt.Printf("Warning: failed to load default value for %s: %v\n", flagName, err)
			}
		}

	case field.Kind() == reflect.String:
		fs.StringVar(field.Addr().Interface().(*string), flagName, defaultValue, usage)

	case field.Kind() == reflect.Int:
		defaultInt, err := strconv.Atoi(defaultValue)
		if err != nil {
			defaultInt = 0
		}
		fs.IntVar(field.Addr().Interface().(*int), flagName, defaultInt, usage)

	case field.Kind() == reflect.Int64:
		defaultInt64, err := strconv.ParseInt(defaultValue, 10, 64)
		if err != nil {
			defaultInt64 = 0
		}
		fs.Int64Var(field.Addr().Interface().(*int64), flagName, defaultInt64, usage)

	case field.Kind() == reflect.Bool:
		defaultBool := strings.ToLower(defaultValue) == "true"
		fs.BoolVar(field.Addr().Interface().(*bool), flagName, defaultBool, usage)

	case field.Kind() == reflect.Float64:
		defaultFloat, err := strconv.ParseFloat(defaultValue, 64)
		if err != nil {
			defaultFloat = 0
		}
		fs.Float64Var(field.Addr().Interface().(*float64), flagName, defaultFloat, usage)

	case field.Kind() == reflect.Uint:
		defaultUint, err := strconv.ParseUint(defaultValue, 10, 0)
		if err != nil {
			defaultUint = 0
		}
		fs.UintVar(field.Addr().Interface().(*uint), flagName, uint(defaultUint), usage)

	case field.Kind() == reflect.Uint64:
		defaultUint64, err := strconv.ParseUint(defaultValue, 10, 64)
		if err != nil {
			defaultUint64 = 0
		}
		fs.Uint64Var(field.Addr().Interface().(*uint64), flagName, defaultUint64, usage)

	default:
		fmt.Printf("Warning: unsupported field type: %s (%s) for field %s\n", field.Kind(), field.Type(), flagName)
	}
}

// autoRegisterWithPrefix 递归注册命令行参数，支持嵌套结构体和 embedding
func (fs *Flags) autoRegisterWithPrefix(config any, envPrefix, flagPrefix string) {
	v := reflect.ValueOf(config)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		logv.Warn().Msgf("config must be a pointer to a struct, got %T", config)
		return
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// 跳过未导出的字段
		if !field.CanSet() {
			continue
		}

		// 处理 embedding 结构体 (匿名字段)
		if fieldType.Anonymous {
			// 对于 embedding 结构体，直接递归处理，不改变前缀
			if field.Kind() == reflect.Struct && field.Type() != reflect.TypeOf(time.Time{}) {
				fs.autoRegisterWithPrefix(field.Addr().Interface(), envPrefix, flagPrefix)
				continue
			}
			// 处理 embedding 指针结构体
			if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Struct {
				if field.IsNil() {
					field.Set(reflect.New(field.Type().Elem()))
				}
				fs.autoRegisterWithPrefix(field.Interface(), envPrefix, flagPrefix)
				continue
			}
		}

		// 获取 json tag 作为参数名
		jsonTag := fieldType.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			// 对于 embedding 结构体，如果没有 json tag，使用字段名
			if fieldType.Anonymous {
				// 对于匿名字段，可以考虑直接跳过或使用类型名
				continue
			}
			continue // 如果没有 json tag 或者是空字符串，则跳过
		} else if strings.Contains(jsonTag, ",") {
			jsonTag = strings.Split(jsonTag, ",")[0]
		}

		// 构建环境变量键名和命令行参数名
		envKey := buildEnvKey(envPrefix, jsonTag)
		flagName := buildFlagName(flagPrefix, jsonTag)

		// 处理普通嵌套结构体（非 embedding）
		if field.Kind() == reflect.Struct && field.Type() != reflect.TypeOf(time.Time{}) && !fieldType.Anonymous {
			fs.autoRegisterWithPrefix(field.Addr().Interface(), envKey, flagName)
			continue
		}

		// 处理指向结构体的指针（非 embedding）
		if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Struct && !fieldType.Anonymous {
			// 如果指针为nil，创建一个新的实例
			if field.IsNil() {
				field.Set(reflect.New(field.Type().Elem()))
			}
			fs.autoRegisterWithPrefix(field.Interface(), envKey, flagName)
			continue
		}

		// 获取 default tag
		defaultTag := fieldType.Tag.Get("default")

		// 获取实际的默认值（优先使用环境变量）
		defaultValue := getDefaultValue(field, envKey, defaultTag)

		// 获取字段的描述信息（优先使用 description 标签）
		usage := getFieldDescription(fieldType, flagName, envKey)

		// 根据字段类型注册不同的参数类型
		fs.registerValue(field, flagName, defaultValue, usage)

		// 注册短标签参数
		if shortTag := fieldType.Tag.Get("short"); shortTag != "" {
			shortUsage := getFieldDescription(fieldType, shortTag, envKey)
			fs.registerValue(field, shortTag, defaultValue, shortUsage)
		}
	}
}

// loadDefaultComplexValue 加载复杂类型的默认值
func loadDefaultComplexValue(field reflect.Value, defaultValue string) error {
	// 首先尝试作为文件路径读取
	if _, err := os.Stat(defaultValue); err == nil {
		data, err := os.ReadFile(defaultValue)
		if err != nil {
			return err
		}
		newValue := reflect.New(field.Type()).Interface()
		if err := json.Unmarshal(data, newValue); err != nil {
			return err
		}
		field.Set(reflect.ValueOf(newValue).Elem())
		return nil
	}

	// 如果不是文件，尝试直接解析为JSON
	newValue := reflect.New(field.Type()).Interface()
	if err := json.Unmarshal([]byte(defaultValue), newValue); err != nil {
		return err
	}
	field.Set(reflect.ValueOf(newValue).Elem())
	return nil
}
