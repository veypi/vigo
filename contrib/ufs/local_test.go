package ufs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// newLocalTestFS 建临时根 localFS。
func newLocalTestFS(t *testing.T) (FS, string) {
	t.Helper()
	root := t.TempDir()
	lfs, err := NewLocalFS(root)
	if err != nil {
		t.Fatal(err)
	}
	return lfs, root
}

// isSymlinkErr 判定错误是否为 symlink 拒绝。
func isSymlinkErr(t *testing.T, err error) bool {
	t.Helper()
	if err == nil {
		return false
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return errors.Is(pe.Err, ErrSymlinkNotSupported)
	}
	return errors.Is(err, ErrSymlinkNotSupported)
}

// TestLocalFSSymlinkRejected 全部操作对 symlink 拒绝（含中间段）：
// 文件本身是 symlink、中间目录是 symlink、以及指向根内的 symlink 均拒绝。
func TestLocalFSSymlinkRejected(t *testing.T) {
	lfs, root := newLocalTestFS(t)

	// 根外目标文件 + 根内普通文件（对照）
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("OK"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 中间段 symlink：dir1/link → root（指向根内目录）
	if err := os.MkdirAll(filepath.Join(root, "dir1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "dir1/link")); err != nil {
		t.Fatal(err)
	}
	// 末段 symlink：leak → 根外文件
	if err := os.Symlink(outside, filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}

	// 末段 symlink：全部读取/写入/管理操作拒绝
	if _, err := lfs.Open("leak"); !isSymlinkErr(t, err) {
		t.Errorf("Open(leak) err = %v, want symlink rejection", err)
	}
	if _, err := lfs.ReadFile("leak"); !isSymlinkErr(t, err) {
		t.Errorf("ReadFile(leak) err = %v, want symlink rejection", err)
	}
	if _, err := lfs.Stat("leak"); !isSymlinkErr(t, err) {
		t.Errorf("Stat(leak) err = %v, want symlink rejection", err)
	}
	if _, err := lfs.ReadDir("leak"); !isSymlinkErr(t, err) {
		t.Errorf("ReadDir(leak) err = %v, want symlink rejection", err)
	}
	if _, err := lfs.Create("leak/new.txt"); !isSymlinkErr(t, err) {
		t.Errorf("Create(leak/new.txt) err = %v, want symlink rejection", err)
	}
	if err := lfs.WriteFile("leak/w.txt", []byte("x"), 0o644); !isSymlinkErr(t, err) {
		t.Errorf("WriteFile(leak/w.txt) err = %v, want symlink rejection", err)
	}
	if err := lfs.MkdirAll("leak/sub", 0o755); !isSymlinkErr(t, err) {
		t.Errorf("MkdirAll(leak/sub) err = %v, want symlink rejection", err)
	}
	if err := lfs.RemoveAll("leak"); !isSymlinkErr(t, err) {
		t.Errorf("RemoveAll(leak) err = %v, want symlink rejection", err)
	}
	if err := lfs.Rename("leak", "renamed"); !isSymlinkErr(t, err) {
		t.Errorf("Rename(leak) err = %v, want symlink rejection", err)
	}
	if err := lfs.Rename("ok.txt", "leak/x"); !isSymlinkErr(t, err) {
		t.Errorf("Rename(dst=leak/x) err = %v, want symlink rejection", err)
	}

	// 中间段 symlink（指向根内也拒绝——语义：UFS 不支持任何 symlink）
	if _, err := lfs.ReadFile("dir1/link/ok.txt"); !isSymlinkErr(t, err) {
		t.Errorf("ReadFile(dir1/link/ok.txt) err = %v, want symlink rejection", err)
	}
	if _, err := lfs.ReadDir("dir1/link"); !isSymlinkErr(t, err) {
		t.Errorf("ReadDir(dir1/link) err = %v, want symlink rejection", err)
	}

	// 正常路径不受影响
	if data, err := lfs.ReadFile("ok.txt"); err != nil || string(data) != "OK" {
		t.Errorf("ReadFile(ok.txt) = %q, %v; want OK", data, err)
	}
	if err := lfs.WriteFile("sub/deep/new.txt", []byte("N"), 0o644); err != nil {
		t.Errorf("WriteFile(新建深路径) err = %v", err)
	}
}

// TestLocalFSSymlinkSearch 搜索（ReadDir/ReadFile 驱动）不泄露 symlink 目标。
func TestLocalFSSymlinkSearch(t *testing.T) {
	lfs, root := newLocalTestFS(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("OUTSIDE-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("INNER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}
	matches, err := lfs.Search(".", "", "SECRET", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if m.Path == "leak" {
			t.Errorf("Search 穿透 symlink 泄露根外内容: %+v", m)
		}
	}
}
