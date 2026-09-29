package flags

import (
	"testing"
)

type Config struct {
	Name    string `json:"name" short:"n" default:"vigo"`
	Port    int    `json:"port" short:"p" default:"8080"`
	Verbose bool   `json:"verbose" short:"v"`
}

func TestAutoRegisterShort(t *testing.T) {
	cfg := &Config{}
	f := New("test", "test flags")
	f.AutoRegister(cfg)
	f.Command = func() error { return nil }
	if err := f.ParseArgs(nil); err != nil {
		t.Fatal(err)
	}

	// Verify long flags
	if f.Lookup("name") == nil {
		t.Error("flag 'name' not registered")
	}
	if f.Lookup("port") == nil {
		t.Error("flag 'port' not registered")
	}

	// Verify short flags
	if f.Lookup("n") == nil {
		t.Error("flag 'n' (short for name) not registered")
	}
	if f.Lookup("p") == nil {
		t.Error("flag 'p' (short for port) not registered")
	}
	if f.Lookup("v") == nil {
		t.Error("flag 'v' (short for verbose) not registered")
	}

	// Verify values match

	// Set short flag
	if err := f.Set("n", "new_name"); err != nil {
		t.Fatalf("failed to set flag 'n': %v", err)
	}
	if cfg.Name != "new_name" {
		t.Errorf("setting short flag 'n' did not update struct field. got %s, want new_name", cfg.Name)
	}

	// Set long flag
	if err := f.Set("port", "9090"); err != nil {
		t.Fatalf("failed to set flag 'port': %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("setting long flag 'port' did not update struct field. got %d, want 9090", cfg.Port)
	}

	// Set short flag for port
	if err := f.Set("p", "9091"); err != nil {
		t.Fatalf("failed to set flag 'p': %v", err)
	}
	if cfg.Port != 9091 {
		t.Errorf("setting short flag 'p' did not update struct field. got %d, want 9091", cfg.Port)
	}
}

type (
	CustomString string
	CustomInt    int
	CustomBool   bool
)
type CustomConfig struct {
	Key     CustomString `json:"key" default:"default-key"`
	Count   CustomInt    `json:"count" default:"10"`
	Enabled CustomBool   `json:"enabled" default:"true"`
}

func TestCustomTypes(t *testing.T) {
	cfg := &CustomConfig{}
	f := New("test_custom", "test custom types")
	f.AutoRegister(cfg)
	f.Command = func() error { return nil }
	if err := f.ParseArgs(nil); err != nil {
		t.Fatal(err)
	}

	// Check default values
	if cfg.Key != "default-key" {
		t.Errorf("expected default key 'default-key', got '%s'", cfg.Key)
	}
	if cfg.Count != 10 {
		t.Errorf("expected default count 10, got %d", cfg.Count)
	}
	if cfg.Enabled != true {
		t.Errorf("expected default enabled true, got %v", cfg.Enabled)
	}

	// Set values via flags
	if err := f.Set("key", "new-key"); err != nil {
		t.Fatalf("failed to set key: %v", err)
	}
	if cfg.Key != "new-key" {
		t.Errorf("expected key 'new-key', got '%s'", cfg.Key)
	}

	if err := f.Set("count", "20"); err != nil {
		t.Fatalf("failed to set count: %v", err)
	}
	if cfg.Count != 20 {
		t.Errorf("expected count 20, got %d", cfg.Count)
	}

	if err := f.Set("enabled", "false"); err != nil {
		t.Fatalf("failed to set enabled: %v", err)
	}
	if cfg.Enabled != false {
		t.Errorf("expected enabled false, got %v", cfg.Enabled)
	}
}

func TestAutoRegisterMultiple(t *testing.T) {
	shared := &Config{}
	opts := &CustomConfig{}
	f := New("test_multi", "test multiple structs")
	f.AutoRegister(shared, opts)
	f.Command = func() error { return nil }
	if err := f.ParseArgs([]string{"-name", "app", "-key", "k1"}); err != nil {
		t.Fatal(err)
	}
	if shared.Name != "app" {
		t.Errorf("expected shared name 'app', got '%s'", shared.Name)
	}
	if opts.Key != "k1" {
		t.Errorf("expected opts key 'k1', got '%s'", opts.Key)
	}
	if shared.Port != 8080 || opts.Count != 10 {
		t.Errorf("expected defaults applied, got port %d count %d", shared.Port, opts.Count)
	}
}

func TestSubCommandInheritsParentFlags(t *testing.T) {
	cfg := &Config{}
	root := New("root", "root command")
	root.AutoRegister(cfg)
	sub := root.SubCommand("sub", "sub command")
	extra := &CustomConfig{}
	sub.AutoRegister(extra)
	sub.Command = func() error { return nil }

	// Parent flags stay valid after the subcommand path, mixed with the
	// subcommand's own flags.
	if err := root.ParseArgs([]string{"sub", "-name", "from-sub", "-key", "k1"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "from-sub" {
		t.Errorf("expected inherited flag to update parent config, got '%s'", cfg.Name)
	}
	if extra.Key != "k1" {
		t.Errorf("expected subcommand flag to update its own options, got '%s'", extra.Key)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
}

func TestSubCommandInheritsShortFlags(t *testing.T) {
	cfg := &Config{}
	root := New("root", "root command")
	root.AutoRegister(cfg)
	sub := root.SubCommand("sub", "sub command")
	sub.Command = func() error { return nil }

	if err := root.ParseArgs([]string{"sub", "-p", "9090"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected inherited short flag to update port, got %d", cfg.Port)
	}
}

func TestInheritedFlagBeatsEnv(t *testing.T) {
	t.Setenv("NAME", "from-env")
	cfg := &Config{}
	root := New("root", "root command")
	root.AutoRegister(cfg)
	sub := root.SubCommand("sub", "sub command")
	sub.Command = func() error { return nil }

	if err := root.ParseArgs([]string{"sub", "-name", "from-flag"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "from-flag" {
		t.Errorf("expected explicit flag to beat env, got '%s'", cfg.Name)
	}
}

func TestLocalFlagShadowsInherited(t *testing.T) {
	cfg := &Config{}
	root := New("root", "root command")
	root.AutoRegister(cfg)
	sub := root.SubCommand("sub", "sub command")
	local := &CustomConfig{}
	sub.AutoRegister(local)
	// CustomConfig has no "name" flag; register a local one to shadow the parent.
	name := sub.String("name", "local-default", "local name")
	sub.Command = func() error { return nil }

	if err := root.ParseArgs([]string{"sub", "-name", "local-value"}); err != nil {
		t.Fatal(err)
	}
	if *name != "local-value" {
		t.Errorf("expected local flag to receive value, got '%s'", *name)
	}
	if cfg.Name != "vigo" {
		t.Errorf("expected parent config untouched by shadowed flag, got '%s'", cfg.Name)
	}
}
