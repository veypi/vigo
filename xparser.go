//
// xparser.go
// Copyright (C) 2025 veypi <i@veypi.com>
// 2025-07-09 02:47
// Distributed under terms of the MIT license.
//

package vigo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

var parserErr = &Error{Code: http.StatusConflict, Message: "parser arg %s with error: %s"}

// Cache structures
type parseSource int

const (
	sourceJSON parseSource = iota
	sourceForm
	sourceQuery
	sourceHeader
	sourcePath
)

type fieldInfo struct {
	Index      int
	Name       string      // key name in source
	Source     parseSource // parse source
	DefaultVal *string     // default value if present
	Field      reflect.StructField
	IsFile     bool
}

type typeInfo struct {
	Fields []fieldInfo
}

var typeCache sync.Map // map[reflect.Type]*typeInfo

func getOrCreateTypeInfo(rt reflect.Type) *typeInfo {
	if v, ok := typeCache.Load(rt); ok {
		return v.(*typeInfo)
	}

	info := &typeInfo{
		Fields: make([]fieldInfo, 0, rt.NumField()),
	}

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		// Skip unexported or anonymous fields
		if field.PkgPath != "" && !field.Anonymous { // PkgPath is non-empty for unexported fields
			continue
		}
		// Note: Anonymous fields are skipped in original code too: "if field.Anonymous { continue }"
		if field.Anonymous {
			continue
		}

		parseTag := field.Tag.Get("src")
		jsonTag := field.Tag.Get("json")

		// Remove options from json tag
		if idx := strings.Index(jsonTag, ","); idx != -1 {
			jsonTag = jsonTag[:idx]
		}
		if jsonTag == "-" {
			continue
		}

		var defaultTag *string
		if tag, ok := field.Tag.Lookup("default"); ok {
			defaultTag = &tag
		}

		if parseTag == "" {
			parseTag = "json"
		}

		fieldName := jsonTag
		if fieldName == "" {
			fieldName = field.Name
		}

		if strings.Contains(parseTag, "@") {
			parts := strings.Split(parseTag, "@")
			parseTag = parts[0]
			if len(parts) > 1 {
				fieldName = parts[1]
			}
		}

		fInfo := fieldInfo{
			Index:      i,
			Name:       fieldName,
			DefaultVal: defaultTag,
			Field:      field,
			IsFile:     isFileType(field.Type),
		}

		switch {
		case parseTag == "json":
			fInfo.Source = sourceJSON
		case parseTag == "form":
			fInfo.Source = sourceForm
		case parseTag == "query":
			fInfo.Source = sourceQuery
		case strings.HasPrefix(parseTag, "header"):
			fInfo.Source = sourceHeader
		case strings.HasPrefix(parseTag, "path"):
			fInfo.Source = sourcePath
		default:
			fInfo.Source = sourceJSON
		}

		info.Fields = append(info.Fields, fInfo)
	}

	v, _ := typeCache.LoadOrStore(rt, info)
	return v.(*typeInfo)
}

// Parse 从 HTTP 请求中解析参数到目标结构体
// 从不同来源解析目标结构体一级字段
// tag标签 src:"path/header/query/form/json" 可以追加为 path@alias_name
// tag标签 default:""
func (x *X) Parse(target any) error {
	parsedJSON := false
	parseJSON := func() error {
		if parsedJSON {
			return nil
		}
		parsedJSON = true

		if x.Request.Body == nil {
			return nil
		}

		// Reset body if needed? No, usually body can only be read once.
		// Assuming this function is called only once or handled correctly.
		err := json.NewDecoder(x.Request.Body).Decode(target)
		if errors.Is(err, io.EOF) {
			// Empty body is not an error
			return nil
		} else if err != nil {
			return ErrArgInvalid.WithArgs(err)
		}
		return nil
	}

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("target must be a pointer to struct: %s", rv.Kind())
	}
	if rv.Elem().Kind() != reflect.Struct {
		return parseJSON()
	}

	// 检查是否需要解析 multipart form（用于文件上传）
	contentType := x.Request.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		if err := x.Request.ParseMultipartForm(32 << 20); err != nil { // 32MB max
			return fmt.Errorf("failed to parse multipart form: %w", err)
		}
	} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		if err := x.Request.ParseForm(); err != nil {
			return fmt.Errorf("failed to parse form: %w", err)
		}
	}

	// 解析 JSON 数据
	if strings.Contains(contentType, "application/json") {
		if err := parseJSON(); err != nil {
			return err
		}
	}

	rv = rv.Elem()
	rt := rv.Type()

	// Use Cached TypeInfo
	info := getOrCreateTypeInfo(rt)

	for _, fieldInfo := range info.Fields {
		fieldValue := rv.Field(fieldInfo.Index)
		if !fieldValue.CanSet() {
			continue
		}

		var value any
		var found bool

		switch fieldInfo.Source {
		case sourceJSON:
			if !parsedJSON {
				if err := parseJSON(); err != nil {
					return err
				}
			}
			continue
		case sourceForm:
			if fieldInfo.IsFile {
				if err := setFileValue(fieldValue, x.Request, fieldInfo.Name); err != nil {
					return parserErr.WithArgs(fieldInfo.Name, err)
				}
				continue
			}
			if x.Request.MultipartForm != nil {
				if fieldValue.Kind() == reflect.Slice && fieldValue.Type().Elem().Kind() == reflect.String {
					value, found = x.Request.MultipartForm.Value[fieldInfo.Name]
				} else if valueList, ok := x.Request.MultipartForm.Value[fieldInfo.Name]; ok {
					value = valueList[0]
					found = true
				}
			} else if x.Request.Form != nil {
				if formValues := x.Request.Form[fieldInfo.Name]; len(formValues) > 0 {
					if fieldValue.Kind() == reflect.Slice && fieldValue.Type().Elem().Kind() == reflect.String {
						value = formValues
					} else {
						value = formValues[0]
					}
					found = true
				}
			}
		case sourceQuery:
			queryValues := x.Request.URL.Query()[fieldInfo.Name]
			if len(queryValues) > 0 {
				if fieldValue.Kind() == reflect.Slice && fieldValue.Type().Elem().Kind() == reflect.String {
					value = queryValues
				} else {
					value = queryValues[0]
				}
				found = true
			}
		case sourceHeader:
			if headerValues := x.Request.Header.Values(fieldInfo.Name); len(headerValues) > 0 {
				value = headerValues[0]
				found = true
			}
		case sourcePath:
			value, found = x.PathParams.Try(fieldInfo.Name)
		}

		if err := setFieldValue(fieldValue, fieldInfo.Name, value, found, fieldInfo.DefaultVal); err != nil {
			return parserErr.WithArgs(fieldInfo.Name, err)
		}
	}

	return nil
}

// isFileType 检查是否是文件类型
func isFileType(t reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 检查是否是 *multipart.FileHeader
	if t.Kind() == reflect.Struct && t.PkgPath() == "mime/multipart" && t.Name() == "FileHeader" {
		return true
	}

	// 检查是否是 []*multipart.FileHeader
	if t.Kind() == reflect.Slice {
		elem := t.Elem()
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct && elem.PkgPath() == "mime/multipart" && elem.Name() == "FileHeader" {
			return true
		}
	}

	return false
}

// setFileValue 设置文件字段值
func setFileValue(fieldValue reflect.Value, req *http.Request, fieldName string) error {
	if req.MultipartForm == nil {
		return nil
	}

	files := req.MultipartForm.File[fieldName]
	if len(files) == 0 {
		return nil
	}

	t := fieldValue.Type()

	// 处理 *multipart.FileHeader
	if t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct {
		if t.Elem().PkgPath() == "mime/multipart" && t.Elem().Name() == "FileHeader" {
			fieldValue.Set(reflect.ValueOf(files[0]))
			return nil
		}
	}

	// 处理 []*multipart.FileHeader
	if t.Kind() == reflect.Slice {
		elem := t.Elem()
		if elem.Kind() == reflect.Ptr && elem.Elem().Kind() == reflect.Struct {
			if elem.Elem().PkgPath() == "mime/multipart" && elem.Elem().Name() == "FileHeader" {
				slice := reflect.MakeSlice(t, len(files), len(files))
				for i, file := range files {
					slice.Index(i).Set(reflect.ValueOf(file))
				}
				fieldValue.Set(slice)
				return nil
			}
		}
	}

	return fmt.Errorf("unsupported file field type: %s", t)
}

// setFieldValue 设置字段值
func setFieldValue(fieldValue reflect.Value, fieldName string, value any, found bool, defaultTag *string) error {
	isPointer := fieldValue.Kind() == reflect.Ptr

	// 如果没有找到值
	if !found || value == nil {
		if defaultTag != nil && *defaultTag != "" {
			// 使用默认值
			return setValueFromString(fieldValue, *defaultTag, isPointer)
		} else if defaultTag != nil {
			return nil
		}
		if !isPointer {
			return ErrArgMissing.WithArgs(fieldName)
		}
		return nil
	}

	// 转换并设置值
	return setValue(fieldValue, value, isPointer)
}

// setValue 设置值到字段
func setValue(fieldValue reflect.Value, value any, isPointer bool) error {
	if isPointer {
		// 处理指针类型
		if fieldValue.IsNil() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}
		fieldValue = fieldValue.Elem()
	}

	switch fieldValue.Kind() {
	case reflect.String:
		strVal, err := convertToString(value)
		if err != nil {
			return err
		}
		fieldValue.SetString(strVal)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := convertToInt(value)
		if err != nil {
			return err
		}
		fieldValue.SetInt(intVal)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := convertToUint(value)
		if err != nil {
			return err
		}
		fieldValue.SetUint(uintVal)

	case reflect.Float32, reflect.Float64:
		floatVal, err := convertToFloat(value)
		if err != nil {
			return err
		}
		fieldValue.SetFloat(floatVal)

	case reflect.Bool:
		boolVal, err := convertToBool(value)
		if err != nil {
			return err
		}
		fieldValue.SetBool(boolVal)

	case reflect.Slice:
		return setSliceValue(fieldValue, value)

	case reflect.Array:
		return setArrayValue(fieldValue, value)

	case reflect.Map:
		return setMapValue(fieldValue, value)

	case reflect.Struct:
		return setStructValue(fieldValue, value)

	case reflect.Interface:
		return setInterfaceValue(fieldValue, value)

	default:
		return fmt.Errorf("unsupported field type: %s", fieldValue.Kind())
	}

	return nil
}

// setSliceValue 设置切片值
func setSliceValue(fieldValue reflect.Value, value any) error {
	elemType := fieldValue.Type().Elem()

	// 处理 []byte
	if elemType.Kind() == reflect.Uint8 {
		byteVal, err := convertToBytes(value)
		if err != nil {
			return err
		}
		fieldValue.SetBytes(byteVal)
		return nil
	}

	// 处理字符串切片
	if elemType.Kind() == reflect.String {
		if strSlice, ok := value.([]string); ok {
			slice := reflect.MakeSlice(fieldValue.Type(), len(strSlice), len(strSlice))
			for i, str := range strSlice {
				slice.Index(i).SetString(str)
			}
			fieldValue.Set(slice)
			return nil
		}
		// 尝试从逗号分隔的字符串解析
		if str, ok := value.(string); ok {
			parts := strings.Split(str, ",")
			slice := reflect.MakeSlice(fieldValue.Type(), len(parts), len(parts))
			for i, part := range parts {
				slice.Index(i).SetString(strings.TrimSpace(part))
			}
			fieldValue.Set(slice)
			return nil
		}
	}

	// 尝试 JSON 解析
	if rawMsg, ok := value.(json.RawMessage); ok {
		if err := json.Unmarshal(rawMsg, fieldValue.Addr().Interface()); err != nil {
			return fmt.Errorf("cannot unmarshal JSON to slice: %w", err)
		}
		return nil
	}

	return fmt.Errorf("cannot convert %T to slice", value)
}

// setArrayValue 设置数组值
func setArrayValue(fieldValue reflect.Value, value any) error {
	// 尝试 JSON 解析
	if rawMsg, ok := value.(json.RawMessage); ok {
		if err := json.Unmarshal(rawMsg, fieldValue.Addr().Interface()); err != nil {
			return fmt.Errorf("cannot unmarshal JSON to array: %w", err)
		}
		return nil
	}

	return fmt.Errorf("cannot convert %T to array", value)
}

// setMapValue 设置映射值
func setMapValue(fieldValue reflect.Value, value any) error {
	if fieldValue.IsNil() {
		fieldValue.Set(reflect.MakeMap(fieldValue.Type()))
	}

	if rawMsg, ok := value.(json.RawMessage); ok {
		if err := json.Unmarshal(rawMsg, fieldValue.Addr().Interface()); err != nil {
			return fmt.Errorf("cannot unmarshal JSON to map: %w", err)
		}
		return nil
	}

	return fmt.Errorf("cannot convert %T to map", value)
}

// setStructValue 设置结构体值
func setStructValue(fieldValue reflect.Value, value any) error {
	// 处理时间类型
	if fieldValue.Type() == reflect.TypeOf(time.Time{}) {
		timeVal, err := convertToTime(value)
		if err != nil {
			return err
		}
		fieldValue.Set(reflect.ValueOf(timeVal))
		return nil
	}

	// 检查是否实现了 json.Unmarshaler 接口
	if unmarshaler, ok := fieldValue.Addr().Interface().(json.Unmarshaler); ok {
		var data []byte
		switch v := value.(type) {
		case json.RawMessage:
			data = v
		case string:
			data = []byte(v)
		case []byte:
			data = v
		default:
			return fmt.Errorf("cannot convert %T to []byte for json.Unmarshaler", value)
		}
		return unmarshaler.UnmarshalJSON(data)
	}

	// 尝试 JSON 解析
	if rawMsg, ok := value.(json.RawMessage); ok {
		if err := json.Unmarshal(rawMsg, fieldValue.Addr().Interface()); err != nil {
			return fmt.Errorf("cannot unmarshal JSON to struct: %w", err)
		}
		return nil
	}

	return fmt.Errorf("cannot convert %T to struct", value)
}

// setInterfaceValue 设置接口值
func setInterfaceValue(fieldValue reflect.Value, value any) error {
	if rawMsg, ok := value.(json.RawMessage); ok {
		var result any
		if err := json.Unmarshal(rawMsg, &result); err != nil {
			return fmt.Errorf("cannot unmarshal JSON to interface{}: %w", err)
		}
		fieldValue.Set(reflect.ValueOf(result))
		return nil
	}

	fieldValue.Set(reflect.ValueOf(value))
	return nil
}

// setValueFromString 从字符串设置默认值
func setValueFromString(fieldValue reflect.Value, strValue string, isPointer bool) error {
	if isPointer {
		if fieldValue.IsNil() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}
		fieldValue = fieldValue.Elem()
	}

	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(strValue)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(strValue, 10, 64)
		if err != nil {
			return fmt.Errorf("cannot parse '%s' as int: %w", strValue, err)
		}
		fieldValue.SetInt(intVal)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(strValue, 10, 64)
		if err != nil {
			return fmt.Errorf("cannot parse '%s' as uint: %w", strValue, err)
		}
		fieldValue.SetUint(uintVal)

	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(strValue, 64)
		if err != nil {
			return fmt.Errorf("cannot parse '%s' as float: %w", strValue, err)
		}
		fieldValue.SetFloat(floatVal)

	case reflect.Bool:
		boolVal, err := strconv.ParseBool(strValue)
		if err != nil {
			return fmt.Errorf("cannot parse '%s' as bool: %w", strValue, err)
		}
		fieldValue.SetBool(boolVal)

	case reflect.Slice:
		if fieldValue.Type().Elem().Kind() == reflect.String {
			parts := strings.Split(strValue, ",")
			slice := reflect.MakeSlice(fieldValue.Type(), len(parts), len(parts))
			for i, part := range parts {
				slice.Index(i).SetString(strings.TrimSpace(part))
			}
			fieldValue.Set(slice)
		} else {
			return fmt.Errorf("unsupported slice type for default value: %s", fieldValue.Type())
		}

	case reflect.Struct:
		if fieldValue.Type() == reflect.TypeOf(time.Time{}) {
			timeVal, err := time.Parse(time.RFC3339, strValue)
			if err != nil {
				return fmt.Errorf("cannot parse '%s' as time: %w", strValue, err)
			}
			fieldValue.Set(reflect.ValueOf(timeVal))
		} else {
			return fmt.Errorf("unsupported struct type for default value: %s", fieldValue.Type())
		}

	default:
		return fmt.Errorf("unsupported field type for default value: %s", fieldValue.Kind())
	}

	return nil
}

// 类型转换辅助函数
func convertToString(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case json.RawMessage:
		// 去掉 JSON 字符串的引号
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			var str string
			if err := json.Unmarshal(v, &str); err != nil {
				return "", err
			}
			return str, nil
		}
		return string(v), nil
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v), nil
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), nil
	case float32, float64:
		return fmt.Sprintf("%g", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	default:
		return "", fmt.Errorf("cannot convert %T to string", value)
	}
}

func convertToBytes(value any) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case json.RawMessage:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to []byte", value)
	}
}

func convertToInt(value any) (int64, error) {
	switch v := value.(type) {
	case string:
		return strconv.ParseInt(v, 10, 64)
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case json.RawMessage:
		var num int64
		if err := json.Unmarshal(v, &num); err != nil {
			return 0, err
		}
		return num, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", value)
	}
}

func convertToUint(value any) (uint64, error) {
	switch v := value.(type) {
	case string:
		return strconv.ParseUint(v, 10, 64)
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case int:
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative int %d to uint", v)
		}
		return uint64(v), nil
	case int8:
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative int8 %d to uint", v)
		}
		return uint64(v), nil
	case int16:
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative int16 %d to uint", v)
		}
		return uint64(v), nil
	case int32:
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative int32 %d to uint", v)
		}
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative int64 %d to uint", v)
		}
		return uint64(v), nil
	case float32:
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative float32 %f to uint", v)
		}
		return uint64(v), nil
	case float64:
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative float64 %f to uint", v)
		}
		return uint64(v), nil
	case json.RawMessage:
		var num uint64
		if err := json.Unmarshal(v, &num); err != nil {
			return 0, err
		}
		return num, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to uint", value)
	}
}

func convertToFloat(value any) (float64, error) {
	switch v := value.(type) {
	case string:
		return strconv.ParseFloat(v, 64)
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case json.RawMessage:
		var num float64
		if err := json.Unmarshal(v, &num); err != nil {
			return 0, err
		}
		return num, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float", value)
	}
}

func convertToBool(value any) (bool, error) {
	switch v := value.(type) {
	case string:
		// 支持更多的布尔值表示
		lower := strings.ToLower(v)
		switch lower {
		case "true", "1", "yes", "on", "y", "t":
			return true, nil
		case "false", "0", "no", "off", "n", "f", "":
			return false, nil
		default:
			return strconv.ParseBool(v)
		}
	case bool:
		return v, nil
	case int, int8, int16, int32, int64:
		return reflect.ValueOf(v).Int() != 0, nil
	case uint, uint8, uint16, uint32, uint64:
		return reflect.ValueOf(v).Uint() != 0, nil
	case float32, float64:
		return reflect.ValueOf(v).Float() != 0, nil
	case json.RawMessage:
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			return false, err
		}
		return b, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", value)
	}
}

func convertToTime(value any) (time.Time, error) {
	switch v := value.(type) {
	case string:
		// 尝试多种时间格式
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
			"15:04:05",
		}

		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t, nil
			}
		}

		// 尝试解析 Unix 时间戳
		if timestamp, err := strconv.ParseInt(v, 10, 64); err == nil {
			if timestamp > 1e10 { // 毫秒时间戳
				return time.Unix(timestamp/1000, (timestamp%1000)*1e6), nil
			} else { // 秒时间戳
				return time.Unix(timestamp, 0), nil
			}
		}

		return time.Time{}, fmt.Errorf("cannot parse time from string: %s", v)

	case int64:
		if v > 1e10 { // 毫秒时间戳
			return time.Unix(v/1000, (v%1000)*1e6), nil
		} else { // 秒时间戳
			return time.Unix(v, 0), nil
		}

	case float64:
		return time.Unix(int64(v), 0), nil

	case json.RawMessage:
		var str string
		if err := json.Unmarshal(v, &str); err == nil {
			return convertToTime(str)
		}
		var timestamp int64
		if err := json.Unmarshal(v, &timestamp); err == nil {
			return convertToTime(timestamp)
		}
		return time.Time{}, fmt.Errorf("cannot parse time from JSON: %s", string(v))

	default:
		return time.Time{}, fmt.Errorf("cannot convert %T to time.Time", value)
	}
}
