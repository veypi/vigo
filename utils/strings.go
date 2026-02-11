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
// 处理连续大写字母的规则：
// 1. 连续大写字母片段作为一个整体处理
// 2. 如果前面是小写字母，连续大写视为新单词（如 aABC -> a_abc）
// 3. 如果后面是小写字母，除最后一个字母外视为一个单词（如 ABCd -> ab_cd）
// 例如：
//   CamelToSnake("CamelToSnake") => "camel_to_snake"
//   CamelToSnake("APIKey") => "api_key"
//   CamelToSnake("AbcID") => "abc_id"
//   CamelToSnake("caseID") => "case_id"
//   CamelToSnake("ABCDef") => "abc_def"
func CamelToSnake(input string) string {
	if input == "" {
		return ""
	}

	runes := []rune(input)
	var result []rune

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if unicode.IsUpper(r) {
			needUnderscore := false

			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					// 前面是小写或数字，大写是新单词的开始，需要下划线
					needUnderscore = true
				} else if unicode.IsUpper(prev) {
					// 前面也是大写，检查是否是连续大写的最后一个
					// 且后面跟着小写字母
					if i < len(runes)-1 && unicode.IsLower(runes[i+1]) {
						// 当前字母和后面小写组成新单词
						needUnderscore = true
					}
				}
			}

			if needUnderscore {
				result = append(result, '_')
			}

			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}

	return string(result)
}

