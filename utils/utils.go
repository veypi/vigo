//
// utils.go
// Copyright (C) 2025 veypi <i@veypi.com>
// 2025-07-15 15:28
// Distributed under terms of the MIT license.
//

package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func CurrentDir(depth ...int) string {
	d := 1
	if len(depth) > 0 {
		d = d + int(depth[0])
	}
	dir := CurrentPath(d)
	if dir == "" {
		return ""
	}
	return filepath.Dir(dir)
}

func CurrentPath(depth ...int) string {
	d := 1
	if len(depth) > 0 {
		d = d + int(depth[0])
	}
	_, filePath, _, ok := runtime.Caller(d)
	if !ok {
		return ""
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return ""
	}
	return absPath
}

func MkFile(dest string) (*os.File, error) {
	if temp, err := filepath.Abs(dest); err == nil {
		dest = temp
	}
	//分割path目录
	destSplitPathDirs := strings.Split(dest, string(filepath.Separator))
	//检测时候存在目录
	destSplitPath := ""
	for _, dir := range destSplitPathDirs[:len(destSplitPathDirs)-1] {
		destSplitPath = destSplitPath + dir + string(filepath.Separator)
		b, _ := PathExists(destSplitPath)
		if !b {
			//创建目录
			_ = os.Mkdir(destSplitPath, 0755)
		}
	}
	// 覆写模式
	return os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
