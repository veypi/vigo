package vigo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAppConfigurationPriorityBeforeInit(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"environment", nil, "env"}, {"command", []string{"-vigo_app_name=cli"}, "cli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VIGO_APP_NAME", "env")
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("name: file\nenabled: false\n"), 0600); err != nil {
				t.Fatal(err)
			}
			previous := os.Args
			os.Args = append([]string{"app", "-f", path}, tc.args...)
			t.Cleanup(func() { os.Args = previous })
			cfg := &struct {
				Name    string `json:"vigo_app_name" yaml:"name" default:"default"`
				Enabled bool   `json:"vigo_app_enabled" yaml:"enabled" default:"true"`
			}{}
			stop := errors.New("stop before starting server")
			app := New("test", NewRouter(), cfg, WithInit(func() error {
				if cfg.Name != tc.want || cfg.Enabled {
					t.Fatalf("incorrect app config: %+v", cfg)
				}
				return stop
			}))
			if err := app.Run(); !errors.Is(err, stop) {
				t.Fatalf("app init: %v", err)
			}
		})
	}
}
