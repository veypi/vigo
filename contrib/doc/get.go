//
// get.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package doc

import (
	"bufio"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/veypi/vigo"
)

func (d *DocFS) Dir(x *vigo.X, opts *DirOpts) (any, error) {
	// 清理路径并添加前缀
	cleanPath := filepath.Clean(opts.Path)
	if cleanPath == "." {
		cleanPath = ""
	}

	fullPath := filepath.Join(d.prefix, cleanPath)

	// 检查路径是否存在以及是文件还是目录
	fileInfo, err := fs.Stat(d.docFS, fullPath)
	if err != nil {
		return nil, ErrFailRead.WithArgs(fullPath, err)
	}

	// 如果是文件，返回文件内容
	if !fileInfo.IsDir() {
		content, err := fs.ReadFile(d.docFS, fullPath)
		if err != nil {
			return nil, ErrFailRead.WithArgs(fullPath, err)
		}

		if strings.HasSuffix(strings.ToLower(fileInfo.Name()), ".md") {
			var toc bool
			if opts.Toc != nil {
				toc = *opts.Toc
			}
			var from, to string
			if opts.From != nil {
				from = *opts.From
			}
			if opts.To != nil {
				to = *opts.To
			}
			return processContent(string(content), toc, from, to), nil
		}

		return string(content), nil
	}

	// 如果是目录，获取目录内容
	var result []*ItemResponse

	// 递归收集文件和目录
	err = d.collectEntries(fullPath, cleanPath, opts.Depth, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func processContent(content string, toc bool, fromStr, toStr string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var result strings.Builder
	// 假设最大支持6级标题
	counters := make([]int, 6)
	// 初始化
	counters[0] = 0 // H1 从 1 开始

	from := parseSectionNumber(fromStr)
	to := parseSectionNumber(toStr)

	// 状态控制
	started := len(from) == 0
	finished := false

	// 正则匹配标题行: ^(#+)\s+(.*)
	re := regexp.MustCompile(`^(#+)\s+(.*)`)

	// 记录代码块状态
	inCodeBlock := false

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
		}

		isHeaderLine := false
		var level int
		var title string
		var currentVer []int

		if !inCodeBlock {
			matches := re.FindStringSubmatch(line)
			if len(matches) == 3 {
				isHeaderLine = true
				level = len(matches[1]) - 1 // # -> 0, ## -> 1
				title = strings.TrimSpace(matches[2])

				if level >= 0 && level < len(counters) {
					// 增加当前层级计数
					counters[level]++

					// 重置所有子层级计数
					for i := level + 1; i < len(counters); i++ {
						counters[i] = 0
					}

					// 获取当前版本号
					currentVer = make([]int, level+1)
					copy(currentVer, counters[:level+1])

					// 检查开始条件
					if !started {
						if compareSections(currentVer, from) >= 0 {
							started = true
						}
					}

					// 检查结束条件
					if started && len(to) > 0 {
						// 如果当前版本号大于 to，且不是 to 的子章节，则结束
						if compareSections(currentVer, to) > 0 && !isDescendant(to, currentVer) {
							finished = true
							break
						}
					}
				}
			}
		}

		if finished {
			break
		}

		if started {
			if toc {
				if isHeaderLine && level >= 0 && level < len(counters) {
					var numbering strings.Builder
					for i := 0; i <= level; i++ {
						fmt.Fprintf(&numbering, "%d.", counters[i])
					}
					indent := strings.Repeat(" ", level)
					result.WriteString(fmt.Sprintf("%s%s %s\n", indent, numbering.String(), title))
				}
			} else {
				result.WriteString(line + "\n")
			}
		}
	}

	return result.String()
}

func parseSectionNumber(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	var res []int
	for _, p := range parts {
		if p == "" {
			continue
		}
		val, _ := strconv.Atoi(p)
		res = append(res, val)
	}
	return res
}

func compareSections(a, b []int) int {
	lenA, lenB := len(a), len(b)
	minLen := lenA
	if lenB < minLen {
		minLen = lenB
	}
	for i := 0; i < minLen; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if lenA < lenB {
		return -1
	}
	if lenA > lenB {
		return 1
	}
	return 0
}

func isDescendant(parent, child []int) bool {
	if len(parent) >= len(child) {
		return false
	}
	for i := 0; i < len(parent); i++ {
		if parent[i] != child[i] {
			return false
		}
	}
	return true
}

func (d *DocFS) collectEntries(fullPath, relativePath string, depth int, result *[]*ItemResponse) error {
	// 如果深度为0，不继续遍历
	if depth == 0 {
		return nil
	}

	// 读取目录内容
	entries, err := fs.ReadDir(d.docFS, fullPath)
	if err != nil {
		return ErrFailRead.WithArgs(fullPath, err)
	}

	// 遍历目录条目
	for _, entry := range entries {
		childFullPath := filepath.Join(fullPath, entry.Name())
		childRelativePath := filepath.Join(relativePath, entry.Name())

		// 仅当是文件时，添加当前条目到结果中
		if !entry.IsDir() {
			*result = append(*result, &ItemResponse{
				Name:     entry.Name(),
				Filename: strings.TrimPrefix(childFullPath, d.prefix),
				IsDir:    false,
			})
		}

		// 如果是目录且需要递归（深度 > 1 或 -1）
		if entry.IsDir() && (depth > 1 || depth == -1) {
			newDepth := depth
			if depth > 1 {
				newDepth = depth - 1
			}

			// 递归收集子目录内容
			err := d.collectEntries(childFullPath, childRelativePath, newDepth, result)
			if err != nil {
				// 记录错误但继续处理其他条目
				continue
			}
		}
	}

	return nil
}
