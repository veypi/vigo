//
// local.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package ufs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrSymlinkNotSupported 表示路径含符号链接：localFS 是受控虚拟文件系统根，
// 不支持下层文件系统的符号链接（os 调用会跟随 symlink，可越权访问根外文件）。
// 任何路径段为 symlink 的操作一律拒绝（fail-closed），写入侧亦无创建 symlink 的 API。
var ErrSymlinkNotSupported = errors.New("ufs: symlinks are not supported")

// MaxNameSegmentLen 是 UFS 路径单段名称长度的硬上限（字节）。所有 UFS 操作
// 读写一视同仁，超出即拒绝（fail-closed）。连锁约束：skill 注册名 ≤32、
// 版本号 ≤16、审核试用副本名最坏 54——64 是给全部用户文件留余量的硬顶。
const MaxNameSegmentLen = 64

// ErrNameTooLong 表示路径中存在超过 MaxNameSegmentLen 的名称段。
// 包装 fs.ErrInvalid（errors.Is 两者皆真）：HTTP 层沿用 fs.ErrInvalid 的
// 400 映射即可，需要差异化文案时可 errors.Is(err, ErrNameTooLong) 判定。
var ErrNameTooLong = fmt.Errorf("%w: name segment exceeds %d bytes", fs.ErrInvalid, MaxNameSegmentLen)

// validatePath normalizes a path and validates it.
// Leading "/" are stripped, "" and "/" become ".".
// 只关心内部定位名的调用方用这个（embed/multi/httpfs/search）。
func validatePath(name, op string) (string, error) {
	inner, _, err := validatePathEcho(name, op)
	return inner, err
}

// validatePathEcho 在 validatePath 之上额外返回调用者原样传入的写法（shown），
// 供报错回显（见 localFS 各方法与 checkNoSymlink 的 shown 参数）：内部去前导 '/'
// 只为满足 fs.ValidPath 语义，但调用方按绝对路径组织（平台 fs 工具、ufs 使用者），
// 报错里出现调用者没给过的相对形态会让其无法定位
// （2026-10-07：fs read 报 "read u/admin/…: no such file or directory"）。
func validatePathEcho(name, op string) (inner, shown string, err error) {
	shown = name
	inner = strings.TrimLeft(name, "/")
	if inner == "" {
		inner = "."
	}
	if !fs.ValidPath(inner) {
		return inner, shown, &fs.PathError{Op: op, Path: shown, Err: fs.ErrInvalid}
	}
	for _, seg := range strings.Split(inner, "/") {
		if len(seg) > MaxNameSegmentLen {
			return inner, shown, &fs.PathError{Op: op, Path: shown, Err: ErrNameTooLong}
		}
	}
	return inner, shown, nil
}

// checkNoSymlink 拒绝任何路径段为符号链接的访问（从根起逐段 Lstat，
// 中间段同样拦截——os 调用会跟随中间段 symlink）。
// 某段不存在时提前放行：不存在路径上不可能有 symlink，且创建类操作的
// 父目录可能尚不存在；本包不提供创建 symlink 的 API，后续写入不会引入。
func (f *localFS) checkNoSymlink(name, shown, op string) error {
	p := f.root
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." {
			continue
		}
		p = filepath.Join(p, seg)
		fi, err := os.Lstat(p)
		if err != nil {
			return nil // 段不存在 → 后续段不存在，放行
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return &fs.PathError{Op: op, Path: shown, Err: ErrSymlinkNotSupported}
		}
	}
	return nil
}

// fsErr extracts the underlying error from OS errors and wraps it in *fs.PathError
// so that the virtual path is exposed instead of the real filesystem path.
func fsErr(err error, op, name string) error {
	if err == nil {
		return nil
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: op, Path: name, Err: pe.Err}
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return &fs.PathError{Op: op, Path: name, Err: le.Err}
	}
	return err
}

// localFS implements FS interface for local file system
type localFS struct {
	root string
}

// NewLocalFS creates a new local file system with full read-write support
func NewLocalFS(root string) (FS, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &localFS{root: root}, nil
}

func (f *localFS) Open(name string) (fs.File, error) {
	inner, shown, err := validatePathEcho(name, "open")
	if err != nil {
		return nil, err
	}
	if err := f.checkNoSymlink(inner, shown, "open"); err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(f.root, inner))
	return file, fsErr(err, "open", shown)
}

func (f *localFS) ReadFile(name string) ([]byte, error) {
	inner, shown, err := validatePathEcho(name, "read")
	if err != nil {
		return nil, err
	}
	if err := f.checkNoSymlink(inner, shown, "read"); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(f.root, inner))
	return data, fsErr(err, "read", shown)
}

func (f *localFS) ReadDir(name string) ([]fs.DirEntry, error) {
	inner, shown, err := validatePathEcho(name, "readdir")
	if err != nil {
		return nil, err
	}
	if err := f.checkNoSymlink(inner, shown, "readdir"); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(f.root, inner))
	return entries, fsErr(err, "readdir", shown)
}

func (f *localFS) Stat(name string) (fs.FileInfo, error) {
	inner, shown, err := validatePathEcho(name, "stat")
	if err != nil {
		return nil, err
	}
	if err := f.checkNoSymlink(inner, shown, "stat"); err != nil {
		return nil, err
	}
	info, err := os.Stat(filepath.Join(f.root, inner))
	return info, fsErr(err, "stat", shown)
}

func (f *localFS) Create(name string) (File, error) {
	inner, shown, err := validatePathEcho(name, "create")
	if err != nil {
		return nil, err
	}
	if err := f.checkNoSymlink(inner, shown, "create"); err != nil {
		return nil, err
	}
	path := filepath.Join(f.root, inner)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fsErr(err, "create", shown)
	}
	file, err := os.Create(path)
	return file, fsErr(err, "create", shown)
}

func (f *localFS) MkdirAll(path string, perm os.FileMode) error {
	inner, shown, err := validatePathEcho(path, "mkdir")
	if err != nil {
		return err
	}
	if err := f.checkNoSymlink(inner, shown, "mkdir"); err != nil {
		return err
	}
	return fsErr(os.MkdirAll(filepath.Join(f.root, inner), perm), "mkdir", shown)
}

func (f *localFS) RemoveAll(path string) error {
	inner, shown, err := validatePathEcho(path, "remove")
	if err != nil {
		return err
	}
	if err := f.checkNoSymlink(inner, shown, "remove"); err != nil {
		return err
	}
	return fsErr(os.RemoveAll(filepath.Join(f.root, inner)), "remove", shown)
}

func (f *localFS) Rename(oldname, newname string) error {
	oldInner, oldShown, err := validatePathEcho(oldname, "rename")
	if err != nil {
		return err
	}
	newInner, newShown, err := validatePathEcho(newname, "rename")
	if err != nil {
		return err
	}
	if err := f.checkNoSymlink(oldInner, oldShown, "rename"); err != nil {
		return err
	}
	if err := f.checkNoSymlink(newInner, newShown, "rename"); err != nil {
		return err
	}
	oldPath := filepath.Join(f.root, oldInner)
	newPath := filepath.Join(f.root, newInner)
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return fsErr(err, "rename", oldShown)
	}
	return fsErr(os.Rename(oldPath, newPath), "rename", oldShown)
}

func (f *localFS) Search(path, glob, pattern string, limit int, ignoreCase bool) ([]SearchMatch, error) {
	return Search(f, path, glob, pattern, limit, ignoreCase)
}

// WriteFile 原子替换写入：同目录暂存 → fsync → rename（暂存提交模式参考
// aic-pod hostfs write）。读者只会观察到替换前或替换后的完整内容，不会读到
// 半截；已存在常规文件保留其权限位，新文件按 perm 创建，失败清理暂存。
func (f *localFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	inner, shown, err := validatePathEcho(name, "write")
	if err != nil {
		return err
	}
	if err := f.checkNoSymlink(inner, shown, "write"); err != nil {
		return err
	}
	path := filepath.Join(f.root, inner)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fsErr(err, "write", shown)
	}
	mode := perm
	if fi, statErr := os.Lstat(path); statErr == nil {
		if fi.Mode().IsRegular() {
			mode = fi.Mode().Perm()
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fsErr(statErr, "write", shown)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ufs-tmp-")
	if err != nil {
		return fsErr(err, "write", shown)
	}
	tmpName := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return fsErr(err, "write", shown)
	}
	return nil
}
