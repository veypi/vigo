package flags

import (
	"flag"
	"io"
	"reflect"
	"strings"
	"testing"
)

type parseOptions struct {
	Name    string   `json:"vigo_test_name" yaml:"name" default:"default" short:"n"`
	Enabled bool     `json:"vigo_test_enabled" yaml:"enabled" default:"true"`
	Count   int      `json:"vigo_test_count" yaml:"count" default:"9"`
	Names   []string `json:"vigo_test_names" yaml:"names" default:"[\"default\"]"`
}

func newConfigFlags(cfg any) *Flags {
	f := New("test", "")
	f.SetOutput(io.Discard)
	f.AutoRegister(cfg)
	f.Command = func() error { return nil }
	return f
}

func TestAutoRegisterDoesNotMutateConfiguration(t *testing.T) {
	t.Setenv("VIGO_TEST_ENABLED", "true")
	cfg := &struct {
		Nested *parseOptions `json:"nested"`
	}{}
	f := newConfigFlags(cfg)
	if cfg.Nested != nil {
		t.Fatal("registration allocated a config pointer")
	}
	if f.Lookup("nested.vigo_test_enabled") == nil {
		t.Fatal("nested flag missing")
	}
}

func TestParseLayerPriority(t *testing.T) {
	path := writeConfig(t, ".yaml", "name: file\nenabled: false\ncount: 0\nnames: []\n")
	for _, tc := range []struct {
		name, env string
		args      []string
		want      string
	}{
		{"file", "", nil, "file"},
		{"env", "environment", nil, "environment"},
		{"cli", "environment", []string{"-vigo_test_name=cli"}, "cli"},
		{"short", "environment", []string{"-n=short"}, "short"},
		{"empty", "environment", []string{"-n="}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &parseOptions{}
			if tc.env != "" {
				t.Setenv("VIGO_TEST_NAME", tc.env)
			}
			f := newConfigFlags(cfg).ConfigFile(path)
			if err := f.ParseArgs(tc.args); err != nil {
				t.Fatal(err)
			}
			if cfg.Name != tc.want || cfg.Enabled || cfg.Count != 0 || len(cfg.Names) != 0 {
				t.Fatalf("priority/zero values broken: %+v", cfg)
			}
		})
	}
}

func TestParseInvalidEnvironmentFallsBackToFile(t *testing.T) {
	t.Setenv("VIGO_TEST_ENABLED", "not-a-bool-secret")
	t.Setenv("VIGO_TEST_COUNT", "bad")
	t.Setenv("VIGO_TEST_NAMES", `["part",{}]`)
	cfg := &parseOptions{}
	f := newConfigFlags(cfg).ConfigFile(writeConfig(t, ".json", `{"vigo_test_enabled":false,"vigo_test_count":3,"vigo_test_names":["file"]}`))
	if err := f.ParseArgs(nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.Count != 3 || !reflect.DeepEqual(cfg.Names, []string{"file"}) {
		t.Fatalf("invalid env replaced file: %+v", cfg)
	}
	if len(f.ConfigIssues()) != 3 {
		t.Fatal(f.ConfigIssues())
	}
	for _, issue := range f.ConfigIssues() {
		if strings.Contains(issue.Reason, "secret") {
			t.Fatal("raw environment value leaked")
		}
	}
}

func TestParseEnvironmentExplicitZeros(t *testing.T) {
	t.Setenv("VIGO_TEST_NAME", "")
	t.Setenv("VIGO_TEST_ENABLED", "false")
	t.Setenv("VIGO_TEST_COUNT", "0")
	t.Setenv("VIGO_TEST_NAMES", "[]")
	cfg := &parseOptions{}
	f := newConfigFlags(cfg).ConfigFile(writeConfig(t, ".yaml", "name: file\nenabled: true\ncount: 4\nnames: [file]"))
	if err := f.ParseArgs(nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "" || cfg.Enabled || cfg.Count != 0 || len(cfg.Names) != 0 {
		t.Fatalf("env zero lost: %+v", cfg)
	}
}

func TestParseBrokenFileStillAppliesEnvironmentAndCLI(t *testing.T) {
	t.Setenv("VIGO_TEST_NAME", "env")
	cfg := &parseOptions{}
	f := newConfigFlags(cfg).ConfigFile(writeConfig(t, ".yaml", "[broken"))
	if err := f.ParseArgs([]string{"-vigo_test_count=0"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "env" || cfg.Count != 0 || !cfg.Enabled {
		t.Fatal(cfg)
	}
}

func TestParseSelectedFileAndSubcommandPreservePriority(t *testing.T) {
	t.Setenv("VIGO_TEST_NAME", "env")
	cfg := &parseOptions{}
	f := newConfigFlags(cfg)
	f.ConfigFileFlag("f", "missing.yaml")
	child := f.SubCommand("child", "")
	child.AutoRegister(cfg)
	child.Command = func() error { return nil }
	path := writeConfig(t, ".yaml", "name: selected-file\nenabled: false\n")
	if err := f.ParseArgs([]string{"-f", path, "-n=parent", "child", "-vigo_test_count=0"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "parent" || cfg.Enabled || cfg.Count != 0 {
		t.Fatalf("child reset parent CLI or file: %+v", cfg)
	}
}

func TestParseManualFlagAliasingConfigField(t *testing.T) {
	cfg := &parseOptions{}
	f := newConfigFlags(cfg).ConfigFile(writeConfig(t, ".yaml", "name: file"))
	child := f.SubCommand("child", "")
	child.StringVar(&cfg.Name, "name", "", "")
	child.Command = func() error { return nil }
	if err := f.ParseArgs([]string{"child", "-name="}); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "" {
		t.Fatal("file overwrote a manual CLI alias")
	}
}

func TestParseAliasesLastValueWinsAndCLIErrorReturns(t *testing.T) {
	cfg := &parseOptions{}
	f := newConfigFlags(cfg)
	if err := f.ParseArgs([]string{"-n=first", "-vigo_test_name=last"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "last" {
		t.Fatal("short/long aliases lost argument order")
	}
	for _, args := range [][]string{{"-unknown"}, {"-vigo_test_count=wrong"}, {"-h"}} {
		f := newConfigFlags(&parseOptions{})
		err := f.ParseArgs(args)
		if err == nil {
			t.Fatalf("bad CLI accepted: %v", args)
		}
		if args[0] == "-h" && err != flag.ErrHelp {
			t.Fatal(err)
		}
	}
}

func TestParseMixedAliasesFollowArgumentOrder(t *testing.T) {
	for _, args := range [][]string{
		{"-manual=first", "-n=last"}, {"-n=first", "-manual=last"},
	} {
		cfg := &parseOptions{}
		f := newConfigFlags(cfg)
		f.StringVar(&cfg.Name, "manual", "", "")
		if err := f.ParseArgs(args); err != nil {
			t.Fatal(err)
		}
		if cfg.Name != "last" {
			t.Fatalf("mixed aliases reordered: %v: %s", args, cfg.Name)
		}
	}
}

type countedText string

var textDecodeCount int

func (v *countedText) UnmarshalText(data []byte) error {
	textDecodeCount++
	*v = countedText(data)
	return nil
}
func TestParseDecodesExplicitValueOnce(t *testing.T) {
	textDecodeCount = 0
	cfg := &struct {
		Value countedText `json:"vigo_counted_value"`
	}{}
	f := newConfigFlags(cfg)
	if err := f.ParseArgs([]string{"-vigo_counted_value=once"}); err != nil {
		t.Fatal(err)
	}
	if textDecodeCount != 1 || cfg.Value != "once" {
		t.Fatalf("CLI decoded %d times", textDecodeCount)
	}
}
