//
// base.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package crud

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/veypi/vigo/logv"
)

// 根据字段标签获取列名
// 获取数据库字段名（优先使用gorm标签，然后是json标签，最后是字段名）
func getColumnName(field reflect.StructField) string {
	// 优先使用gorm标签
	if gormTag := field.Tag.Get("gorm"); gormTag != "" {
		for _, tag := range strings.Split(gormTag, ";") {
			tag = strings.TrimSpace(tag)
			if strings.HasPrefix(tag, "column:") {
				return strings.TrimPrefix(tag, "column:")
			}
		}
	}

	// 然后使用json标签
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		// 处理json标签中的选项，如 "name,omitempty"
		if idx := strings.Index(jsonTag, ","); idx != -1 {
			return jsonTag[:idx]
		}
		return jsonTag
	}

	// 最后使用字段名
	return field.Name
}

// 更好的类型转换函数
func convertFieldType(value interface{}, targetType reflect.Type) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	// 如果类型已经匹配，直接返回
	valueType := reflect.TypeOf(value)
	if valueType == targetType {
		return value, nil
	}

	// 获取目标类型的基本类型（处理指针）
	target := targetType
	if target.Kind() == reflect.Ptr {
		target = target.Elem()
	}

	// 处理指针类型
	if targetType.Kind() == reflect.Ptr {
		converted, err := convertBasicType(value, target)
		if err != nil {
			return nil, err
		}

		// 创建指针
		ptr := reflect.New(target)
		ptr.Elem().Set(reflect.ValueOf(converted))
		return ptr.Interface(), nil
	}

	return convertBasicType(value, target)
}

// 基本类型转换函数
func convertBasicType(value interface{}, targetType reflect.Type) (interface{}, error) {
	switch targetType.Kind() {
	case reflect.String:
		return toString(value), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return toInt(value, targetType)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return toUint(value, targetType)
	case reflect.Float32, reflect.Float64:
		return toFloat(value, targetType)
	case reflect.Bool:
		return toBool(value), nil
	case reflect.Struct:
		return convertStruct(value, targetType)
	default:
		logv.Warn().Msgf("\n:%v:%v", value, targetType.Kind())
		// 对于不支持的类型，返回原值
		return value, nil
	}
}

// 转换为字符串
func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%s", v)
	}
}

// 转换为整数
func toInt(value interface{}, targetType reflect.Type) (interface{}, error) {
	switch v := value.(type) {
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, err
		}
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case int:
		switch targetType.Kind() {
		case reflect.Int:
			return v, nil
		case reflect.Int8:
			return int8(v), nil
		case reflect.Int16:
			return int16(v), nil
		case reflect.Int32:
			return int32(v), nil
		case reflect.Int64:
			return int64(v), nil
		}
	case int8:
		switch targetType.Kind() {
		case reflect.Int:
			return int(v), nil
		case reflect.Int8:
			return v, nil
		case reflect.Int16:
			return int16(v), nil
		case reflect.Int32:
			return int32(v), nil
		case reflect.Int64:
			return int64(v), nil
		}
	case int16:
		switch targetType.Kind() {
		case reflect.Int:
			return int(v), nil
		case reflect.Int8:
			return int8(v), nil
		case reflect.Int16:
			return v, nil
		case reflect.Int32:
			return int32(v), nil
		case reflect.Int64:
			return int64(v), nil
		}
	case int32:
		switch targetType.Kind() {
		case reflect.Int:
			return int(v), nil
		case reflect.Int8:
			return int8(v), nil
		case reflect.Int16:
			return int16(v), nil
		case reflect.Int32:
			return v, nil
		case reflect.Int64:
			return int64(v), nil
		}
	case int64:
		switch targetType.Kind() {
		case reflect.Int:
			return int(v), nil
		case reflect.Int8:
			return int8(v), nil
		case reflect.Int16:
			return int16(v), nil
		case reflect.Int32:
			return int32(v), nil
		case reflect.Int64:
			return v, nil
		}
	case uint:
		i := int64(v)
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case uint64:
		i := int64(v)
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case uint32:
		i := int64(v)
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case uint16:
		i := int64(v)
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case uint8:
		i := int64(v)
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case float32:
		i := int64(v)
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case float64:
		i := int64(v)
		switch targetType.Kind() {
		case reflect.Int:
			return int(i), nil
		case reflect.Int8:
			return int8(i), nil
		case reflect.Int16:
			return int16(i), nil
		case reflect.Int32:
			return int32(i), nil
		case reflect.Int64:
			return i, nil
		}
	case bool:
		if v {
			switch targetType.Kind() {
			case reflect.Int:
				return 1, nil
			case reflect.Int8:
				return int8(1), nil
			case reflect.Int16:
				return int16(1), nil
			case reflect.Int32:
				return int32(1), nil
			case reflect.Int64:
				return int64(1), nil
			}
		} else {
			switch targetType.Kind() {
			case reflect.Int:
				return 0, nil
			case reflect.Int8:
				return int8(0), nil
			case reflect.Int16:
				return int16(0), nil
			case reflect.Int32:
				return int32(0), nil
			case reflect.Int64:
				return int64(0), nil
			}
		}
	}

	return 0, fmt.Errorf("cannot convert %T to %s", value, targetType.Kind())
}

// 转换为无符号整数
func toUint(value interface{}, targetType reflect.Type) (interface{}, error) {
	switch v := value.(type) {
	case string:
		u, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, err
		}
		switch targetType.Kind() {
		case reflect.Uint:
			return uint(u), nil
		case reflect.Uint8:
			return uint8(u), nil
		case reflect.Uint16:
			return uint16(u), nil
		case reflect.Uint32:
			return uint32(u), nil
		case reflect.Uint64:
			return u, nil
		}
	case int:
		if v < 0 {
			return 0, fmt.Errorf("negative value %d cannot be converted to unsigned integer", v)
		}
		u := uint64(v)
		switch targetType.Kind() {
		case reflect.Uint:
			return uint(u), nil
		case reflect.Uint8:
			return uint8(u), nil
		case reflect.Uint16:
			return uint16(u), nil
		case reflect.Uint32:
			return uint32(u), nil
		case reflect.Uint64:
			return u, nil
		}
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("negative value %d cannot be converted to unsigned integer", v)
		}
		u := uint64(v)
		switch targetType.Kind() {
		case reflect.Uint:
			return uint(u), nil
		case reflect.Uint8:
			return uint8(u), nil
		case reflect.Uint16:
			return uint16(u), nil
		case reflect.Uint32:
			return uint32(u), nil
		case reflect.Uint64:
			return u, nil
		}
	case uint:
		switch targetType.Kind() {
		case reflect.Uint:
			return v, nil
		case reflect.Uint8:
			return uint8(v), nil
		case reflect.Uint16:
			return uint16(v), nil
		case reflect.Uint32:
			return uint32(v), nil
		case reflect.Uint64:
			return uint64(v), nil
		}
	case uint64:
		switch targetType.Kind() {
		case reflect.Uint:
			return uint(v), nil
		case reflect.Uint8:
			return uint8(v), nil
		case reflect.Uint16:
			return uint16(v), nil
		case reflect.Uint32:
			return uint32(v), nil
		case reflect.Uint64:
			return v, nil
		}
	case bool:
		if v {
			switch targetType.Kind() {
			case reflect.Uint:
				return uint(1), nil
			case reflect.Uint8:
				return uint8(1), nil
			case reflect.Uint16:
				return uint16(1), nil
			case reflect.Uint32:
				return uint32(1), nil
			case reflect.Uint64:
				return uint64(1), nil
			}
		} else {
			switch targetType.Kind() {
			case reflect.Uint:
				return uint(0), nil
			case reflect.Uint8:
				return uint8(0), nil
			case reflect.Uint16:
				return uint16(0), nil
			case reflect.Uint32:
				return uint32(0), nil
			case reflect.Uint64:
				return uint64(0), nil
			}
		}
	}

	return 0, fmt.Errorf("cannot convert %T to %s", value, targetType.Kind())
}

// 转换为浮点数
func toFloat(value interface{}, targetType reflect.Type) (interface{}, error) {
	switch v := value.(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, err
		}
		switch targetType.Kind() {
		case reflect.Float32:
			return float32(f), nil
		case reflect.Float64:
			return f, nil
		}
	case float32:
		switch targetType.Kind() {
		case reflect.Float32:
			return v, nil
		case reflect.Float64:
			return float64(v), nil
		}
	case float64:
		switch targetType.Kind() {
		case reflect.Float32:
			return float32(v), nil
		case reflect.Float64:
			return v, nil
		}
	case int:
		f := float64(v)
		switch targetType.Kind() {
		case reflect.Float32:
			return float32(f), nil
		case reflect.Float64:
			return f, nil
		}
	case int64:
		f := float64(v)
		switch targetType.Kind() {
		case reflect.Float32:
			return float32(f), nil
		case reflect.Float64:
			return f, nil
		}
	case uint:
		f := float64(v)
		switch targetType.Kind() {
		case reflect.Float32:
			return float32(f), nil
		case reflect.Float64:
			return f, nil
		}
	case bool:
		if v {
			switch targetType.Kind() {
			case reflect.Float32:
				return float32(1), nil
			case reflect.Float64:
				return 1.0, nil
			}
		} else {
			switch targetType.Kind() {
			case reflect.Float32:
				return float32(0), nil
			case reflect.Float64:
				return 0.0, nil
			}
		}
	}

	return 0, fmt.Errorf("cannot convert %T to %s", value, targetType.Kind())
}

// 转换为布尔值
func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	case int:
		return v != 0
	case int64:
		return v != 0
	case float32:
		return v != 0
	case float64:
		return v != 0
	case uint:
		return v != 0
	case uint64:
		return v != 0
	default:
		return false
	}
}

// 转换结构体（主要是time.Time）
func convertStruct(value interface{}, targetType reflect.Type) (interface{}, error) {
	// 特殊处理time.Time类型
	if targetType == reflect.TypeOf(time.Time{}) {
		switch v := value.(type) {
		case string:
			// 尝试多种时间格式
			formats := []string{
				"2006-01-02 15:04:05",
				"2006-01-02T15:04:05Z",
				"2006-01-02T15:04:05.000Z",
				time.RFC3339,
				time.RFC3339Nano,
			}

			for _, format := range formats {
				if t, err := time.Parse(format, v); err == nil {
					return t, nil
				}
			}

			// 如果都不是标准格式，尝试解析为时间戳
			if timestamp, err := strconv.ParseInt(v, 10, 64); err == nil {
				return time.Unix(timestamp, 0), nil
			}
			if timestamp, err := strconv.ParseFloat(v, 64); err == nil {
				return time.Unix(int64(timestamp), 0), nil
			}

			return time.Time{}, fmt.Errorf("cannot parse time from string: %s", v)
		case int64:
			return time.Unix(v, 0), nil
		case float64:
			return time.Unix(int64(v), 0), nil
		}
	}

	return value, nil
}
