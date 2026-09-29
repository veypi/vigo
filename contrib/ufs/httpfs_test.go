package ufs

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestIsRawRequest raw/SPA 回落判定契约：X-No-Fallback 头、?x-no-fallback、
// Accept 不含 text/html（含无 Accept 的 curl/组件 loader）→ raw 原样资源；
// 浏览器页面导航（Accept: text/html、无上述标记）→ 非 raw，由 SPA 壳接管。
func TestIsRawRequest(t *testing.T) {
	cases := []struct {
		name string
		url  string
		hdr  map[string]string
		want bool
	}{
		{"no accept header (curl/loader)", "/x.html", nil, true},
		{"star accept (fetch default)", "/x.html", map[string]string{"Accept": "*/*"}, true},
		{"json accept", "/x.html", map[string]string{"Accept": "application/json"}, true},
		{"x-no-fallback header", "/x.html", map[string]string{"Accept": "text/html", "X-No-Fallback": "1"}, true},
		{"x-no-fallback query", "/x.html?x-no-fallback=1", map[string]string{"Accept": "text/html"}, true},
		{"browser navigation", "/x.html?id=3", map[string]string{"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", c.url, nil)
			for k, v := range c.hdr {
				r.Header.Set(k, v)
			}
			if got := IsRawRequest(r); got != c.want {
				t.Errorf("IsRawRequest(%q, %v) = %v, want %v", c.url, c.hdr, got, c.want)
			}
		})
	}
}

// TestGitRepoInfo 仓库探测与分支解析：.git 为目录 → 仓库成立；HEAD 为
// "ref: refs/heads/<name>" 时给出分支名；detached HEAD / HEAD 缺失 / .git
// 为文件（worktree/submodule 形态）→ 分支为空或不识别为仓库；
// buildItemTree 输出同字段（目录列表 JSON 形态）。
func TestGitRepoInfo(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("repo/.git/HEAD", "ref: refs/heads/feature/x\n")
	write("det/.git/HEAD", "0123456789abcdef0123456789abcdef01234567\n")
	write("nohead/.git/config", "")
	write("plain/a.txt", "x")
	write("wt/.git", "gitdir: /elsewhere\n")

	fsys := os.DirFS(root)
	cases := []struct {
		path   string
		repo   bool
		branch string
	}{
		{"repo", true, "feature/x"},
		{"det", true, ""},
		{"nohead", true, ""},
		{"plain", false, ""},
		{"wt", false, ""},
	}
	for _, c := range cases {
		repo, branch := gitRepoInfo(fsys, c.path)
		if repo != c.repo || branch != c.branch {
			t.Errorf("gitRepoInfo(%q) = (%v, %q), want (%v, %q)", c.path, repo, branch, c.repo, c.branch)
		}
	}

	// 生产路径同源：localFS（ufs.FS，/fs/cloud 实际后端）同样可用
	lfs, err := NewLocalFS(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		repo, branch := gitRepoInfo(lfs, c.path)
		if repo != c.repo || branch != c.branch {
			t.Errorf("gitRepoInfo(localFS, %q) = (%v, %q), want (%v, %q)", c.path, repo, branch, c.repo, c.branch)
		}
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := buildItemTree(fsys, ".", info, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range entry.Items {
		if it.Name != "repo" {
			continue
		}
		found = true
		if !it.IsRepo || it.Branch != "feature/x" {
			t.Errorf("listing repo: is_repo=%v branch=%q, want true/feature/x", it.IsRepo, it.Branch)
		}
	}
	if !found {
		t.Fatal("listing: repo entry not found")
	}
}
