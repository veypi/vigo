//
// strings.go
// Copyright (C) 2025 veypi <i@veypi.com>
// 2025-07-15 15:53
// Distributed under terms of the MIT license.
//

package utils

import (
	"strings"
	"unicode"
)

func ToTitle(str string) string {
	if str == "" {
		return str
	}
	runes := []rune(str)
	runes[0] = unicode.ToUpper(runes[0])
	for i := 1; i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

// ToLowerFirst 将字符串的第一个字母转换为小写
func ToLowerFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	firstRune := unicode.ToLower(rune(s[0]))
	return string(firstRune) + s[1:]
}
func ToUpperFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	firstRune := unicode.ToUpper(rune(s[0]))
	return string(firstRune) + s[1:]
}

func SnakeToPrivateCamel(input string) string {
	parts := strings.Split(input, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = ToTitle(parts[i])
	}
	parts[0] = strings.ToLower(parts[0])
	return strings.Join(parts, "")
}
func SnakeToCamel(input string) string {
	parts := strings.Split(input, "_")
	for i := 0; i < len(parts); i++ {
		parts[i] = ToTitle(parts[i])
		if parts[i] == "Id" {
			parts[i] = "ID"
		}
	}
	return strings.Join(parts, "")
}

// CamelToSnake 将驼峰命名法转换为下划线命名法
// 例如：CamelToSnake("CamelToSnake") => "camel_to_snake"
// special case: CaseID => case_id
func CamelToSnake(input string) string {
	var result []rune
	input = strings.ReplaceAll(input, "ID", "Id")
	for i, r := range input {
		if unicode.IsUpper(r) {
			// 在大写字母前面添加下划线，除非它是第一个字母
			if i > 0 {
				result = append(result, '_')
			}
			// 转换为小写字母
			r = unicode.ToLower(r)
		}
		result = append(result, r)
	}
	return string(result)
}
