package ufs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// TestValidatePathSegmentLength 单段名称长度硬校验：≤64 放行、>64 拒绝（读写同规则），
// 错误同时满足 errors.Is(fs.ErrInvalid) 与 errors.Is(ErrNameTooLong)。
func TestValidatePathSegmentLength(t *testing.T) {
	lfs, _ := newLocalTestFS(t)
	seg64 := strings.Repeat("a", MaxNameSegmentLen)
	seg65 := strings.Repeat("b", MaxNameSegmentLen+1)
	long := func(base string) string { return "dir/" + base }

	// 边界：64 字节单段读写均放行。
	if err := lfs.WriteFile(long(seg64), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(64-byte segment) err = %v; want nil", err)
	}
	if _, err := lfs.ReadFile(long(seg64)); err != nil {
		t.Fatalf("ReadFile(64-byte segment) err = %v; want nil", err)
	}

	// 超限：65 字节单段读写均拒绝，且多段路径只拦超长段。
	for _, op := range []struct {
		name string
		run  func(path string) error
	}{
		{"ReadFile", func(p string) error { _, err := lfs.ReadFile(p); return err }},
		{"WriteFile", func(p string) error { return lfs.WriteFile(p, []byte("x"), 0o644) }},
		{"Stat", func(p string) error { _, err := lfs.Stat(p); return err }},
		{"RemoveAll", func(p string) error { return lfs.RemoveAll(p) }},
	} {
		for _, path := range []string{long(seg65), seg65 + "/ok.txt"} {
			err := op.run(path)
			if err == nil {
				t.Errorf("%s(%q) err = nil; want segment-length rejection", op.name, path)
				continue
			}
			if !errors.Is(err, fs.ErrInvalid) || !errors.Is(err, ErrNameTooLong) {
				t.Errorf("%s(%q) err = %v; want Is(fs.ErrInvalid)+Is(ErrNameTooLong)", op.name, path, err)
			}
		}
	}

	// 根目录（"."）与常规路径不受影响。
	if _, err := lfs.Stat("."); err != nil {
		t.Errorf("Stat(root) err = %v; want nil", err)
	}
	if err := lfs.WriteFile("a/b/c.txt", []byte("x"), 0o644); err != nil {
		t.Errorf("WriteFile(a/b/c.txt) err = %v; want nil", err)
	}
}

// TestLocalFSErrorEchoesCallerPath 报错回显调用者传入的路径形态（含前导 '/'）：
// localFS 内部去掉前导 '/' 只是为满足 fs.ValidPath 语义，但报错必须与调用者所见
// 一致——上层（平台 fs 工具、ufs 使用者）按绝对路径组织，回显被去前缀的相对形态
// 会让调用方无法定位（2026-10-07：fs read 报 "read u/admin/…: no such file…"）。
func TestLocalFSErrorEchoesCallerPath(t *testing.T) {
	lfs, _ := newLocalTestFS(t)
	for _, c := range []struct {
		op   string
		path string
		run  func(path string) error
	}{
		{"ReadFile", "/dir/absent.txt", func(p string) error { _, err := lfs.ReadFile(p); return err }},
		{"Stat", "/dir/absent.txt", func(p string) error { _, err := lfs.Stat(p); return err }},
		{"ReadDir", "/absent-dir", func(p string) error { _, err := lfs.ReadDir(p); return err }},
		{"Open", "/absent-dir/absent.txt", func(p string) error { _, err := lfs.Open(p); return err }},
	} {
		err := c.run(c.path)
		if err == nil {
			t.Errorf("%s(%q) err = nil; want not-exist error", c.op, c.path)
			continue
		}
		var pe *fs.PathError
		if !errors.As(err, &pe) {
			t.Errorf("%s(%q) err = %v (%T); want *fs.PathError", c.op, c.path, err, err)
			continue
		}
		if pe.Path != c.path {
			t.Errorf("%s 报错回显路径 = %q; want %q", c.op, pe.Path, c.path)
		}
		if !strings.Contains(err.Error(), c.path) {
			t.Errorf("%s 报错文本 %q 未含调用者路径 %q", c.op, err.Error(), c.path)
		}
	}

	// 无前导 '/' 的调用形态原样回显（不擅自绝对化）。
	if err := func() error { _, err := lfs.Stat("dir/absent.txt"); return err }(); err == nil {
		t.Error("Stat(relative absent) err = nil; want error")
	} else {
		var pe *fs.PathError
		if !errors.As(err, &pe) || pe.Path != "dir/absent.txt" {
			t.Errorf("相对路径回显 = %v; want PathError.Path = %q", err, "dir/absent.txt")
		}
	}

	// 写失败同样回显调用者路径（目标为目录：原子替换必失败）。
	if err := lfs.MkdirAll("/wdir", 0o755); err != nil {
		t.Fatal(err)
	}
	err := lfs.WriteFile("/wdir", []byte("x"), 0o644)
	if err == nil {
		t.Fatal("WriteFile over a directory must fail")
	}
	var pe *fs.PathError
	if !errors.As(err, &pe) || pe.Path != "/wdir" {
		t.Errorf("WriteFile 报错 = %v; want PathError.Path = %q", err, "/wdir")
	}
}

// TestLocalFSWriteFileAtomic 原子替换写语义：覆盖正确、既有权限保留、新文件
// 用 perm、空内容可写、失败（目标为目录）报错，且成功/失败路径均无暂存残留。
func TestLocalFSWriteFileAtomic(t *testing.T) {
	lfs, root := newLocalTestFS(t)

	// 覆盖写：内容整体替换。
	if err := lfs.WriteFile("a.txt", []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lfs.WriteFile("a.txt", []byte("v2-longer-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := lfs.ReadFile("a.txt"); err != nil || string(data) != "v2-longer-content" {
		t.Fatalf("overwrite = %q, %v", data, err)
	}

	// 既有权限保留：chmod 0640 后覆盖写，权限不变。
	if err := os.Chmod(filepath.Join(root, "a.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := lfs.WriteFile("a.txt", []byte("v3"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(root, "a.txt")); err != nil || fi.Mode().Perm() != 0o640 {
		t.Fatalf("existing mode must be preserved: %v", err)
	}

	// 新文件按 perm 创建。
	if err := lfs.WriteFile("b.txt", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(root, "b.txt")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("new file mode: %v", err)
	}

	// 空内容写入。
	if err := lfs.WriteFile("empty.txt", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := lfs.ReadFile("empty.txt"); err != nil || len(data) != 0 {
		t.Fatalf("empty write = %q, %v", data, err)
	}

	// 目标为目录：报错且不残留暂存。
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := lfs.WriteFile("dir", []byte("x"), 0o644); err == nil {
		t.Fatal("writing over a directory must fail")
	}

	// 全树无 .ufs-tmp-* 残留。
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".ufs-tmp-") {
			t.Errorf("staging residue: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
