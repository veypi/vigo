package ufs

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestFS(t *testing.T) {
	// 1. Prepare local directory
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "local_only.txt"), []byte("local"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "override.txt"), []byte("local_override"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "dir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "dir", "file.txt"), []byte("dir_file"), 0644)

	// 2. Prepare mock FS 1 (simulating simple embed)
	mockFS1 := fstest.MapFS{
		"embed_only.txt": {Data: []byte("embed")},
		"override.txt":   {Data: []byte("embed_override")}, // Should be shadowed by local
	}

	// 3. Prepare mock FS 2 (simulating embed with prefix)
	// We want to mount this under nothing, but use fs.Sub logic.
	// If user calls Embed(fs, "assets"), it means fs.Sub(fs, "assets").
	// So the mock fs should HAVE "assets/..." structure.
	mockFS2 := fstest.MapFS{
		"assets/nested.txt": {Data: []byte("nested")},
		"assets/deep/a.txt": {Data: []byte("deep")},
	}

	// 4. Create UFS
	// Order: Local -> MockFS1 -> EmbedOpt(MockFS2, "assets")
	myFS := New(
		tmpDir,
		mockFS1,
		Embed(mockFS2, "assets"),
	)

	// 5. Run tests
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{"Local file", "local_only.txt", "local", false},
		{"Embed file", "embed_only.txt", "embed", false},
		{"Override file", "override.txt", "local_override", false},
		{"Nested via EmbedOpt", "nested.txt", "nested", false}, // "assets/nested.txt" became "nested.txt"
		{"Non-existent", "ghost.txt", "", true},
		{"Dir file", "dir/file.txt", "dir_file", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := fs.ReadFile(myFS, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && string(content) != tt.want {
				t.Errorf("ReadFile() content = %s, want %s", content, tt.want)
			}
		})
	}

	// 6. Test ReadDir
	t.Run("ReadDir Root", func(t *testing.T) {
		entries, err := fs.ReadDir(myFS, ".")
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		// Expect only files from tmpDir because it handles "." successfully
		expected := map[string]bool{
			"local_only.txt": true,
			"override.txt":   true,
			"dir":            true,
		}

		if len(entries) != len(expected) {
			t.Errorf("ReadDir count = %d, want %d", len(entries), len(expected))
		}

		for _, e := range entries {
			if !expected[e.Name()] {
				t.Errorf("Unexpected entry: %s", e.Name())
			}
		}
	})

	// Test ReadDir for a path only in deeper layer
	t.Run("ReadDir Deep", func(t *testing.T) {
		// "deep" folder is only in mockFS2 (under assets/deep -> becomes deep)
		entries, err := fs.ReadDir(myFS, "deep")
		if err != nil {
			t.Fatalf("ReadDir deep failed: %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "a.txt" {
			t.Errorf("ReadDir deep unexpected: %v", entries)
		}
	})
}
