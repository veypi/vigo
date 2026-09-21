//
// flags.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-08-12 14:52
// Distributed under terms of the MIT license.
//

package flags

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/joho/godotenv"
	"github.com/veypi/vigo/utils"
	"gopkg.in/yaml.v3"
)

func New(name string, des string) *Flags {
	f := &Flags{
		FlagSet: *flag.NewFlagSet(name, flag.ContinueOnError),
		des:     des,
		depth:   0,
	}
	return f
}

type Flags struct {
	flag.FlagSet
	des             string
	depth           int
	subs            []*Flags
	parent          *Flags
	help            string
	runFunc         func() error
	Command         func() error
	Before          func() error
	After           func(error) error
	allowArgs       bool // AllowArgs 开启后允许位置参数（Command 内经 f.Args() 读取）
	configs         []any
	bindings        []*boundValue
	configFile      *string
	issues          []ConfigIssue
	registrationErr error
	collecting      bool
	explicit        []func()
	parsed          bool
}

// AllowArgs 允许本命令携带位置参数（默认拒绝）。
// 常用于子命令场景：如 `aic bind <key>`、`aic config set <key> <value>`。
func (f *Flags) AllowArgs() *Flags {
	f.allowArgs = true
	return f
}

func (f *Flags) Run() (err error) {
	if f.parent != nil {
		return f.parent.Run()
	}
	if f.runFunc == nil {
		return fmt.Errorf("you need call Parse() first")
	}
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v:\n%s", e, debug.Stack())
		}
	}()
	err = f.runFunc()
	return err
}

// Parse resolves registered configuration and command-line arguments once.
// It returns flag.ErrHelp for help and never exits the calling process.
func (f *Flags) Parse() error { return f.ParseArgs(os.Args[1:]) }

// ConfigFile declares the optional file for configurations registered on f.
func (f *Flags) ConfigFile(path string) *Flags { f.configFile = &path; return f }

// ConfigFileFlag declares a file selector such as -f. Its value is resolved by
// the normal argument parser before configuration layers are applied.
func (f *Flags) ConfigFileFlag(name, fallback string) *string {
	f.configFile = f.String(name, fallback, "configuration file")
	return f.configFile
}

// ConfigIssues returns ignored file/env/default problems without raw values.
func (f *Flags) ConfigIssues() []ConfigIssue { return append([]ConfigIssue(nil), f.issues...) }

// ParseArgs applies defaults < file < environment < explicit command-line values.
// AutoRegister does not assign values, and each argument is decoded only once.
func (f *Flags) ParseArgs(arguments []string) error {
	if f.parsed {
		return fmt.Errorf("flags have already been parsed")
	}
	f.parsed = true
	_ = godotenv.Load()
	var chain []*Flags
	selected, err := f.selectCommand(arguments, &chain)
	if err != nil {
		return err
	}
	seen := make(map[any]bool)
	for _, scope := range chain {
		for _, cfg := range scope.configs {
			applyDefaults(reflect.ValueOf(cfg), "", seen, &f.issues)
		}
	}
	for _, scope := range chain {
		if scope.configFile != nil {
			for _, cfg := range scope.configs {
				f.issues = append(f.issues, loadConfigFile(*scope.configFile, cfg)...)
			}
		}
	}
	for _, scope := range chain {
		for _, binding := range scope.bindings {
			if raw, exists := os.LookupEnv(binding.env); exists {
				value, err := parseValue(binding.typ, raw)
				if err != nil {
					f.issues = append(f.issues, ConfigIssue{"env", binding.name, "invalid environment value"})
				} else {
					binding.field(true).Set(value)
				}
			}
		}
	}
	for _, scope := range chain {
		for _, apply := range scope.explicit {
			apply()
		}
	}
	if selected.Command == nil {
		selected.Usage()
		return flag.ErrHelp
	}
	if !selected.allowArgs && selected.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", selected.Arg(0))
	}
	selected.set_run_func(func() error {
		if err := selected.fire_before_func(); err != nil {
			return err
		}
		return selected.fire_after_func(selected.Command())
	})
	return nil
}

func (f *Flags) str() string {
	if f.parent == nil {
		return f.Name()
	}
	return f.parent.str() + " " + f.Name()
}

func (f *Flags) fire_before_func() error {
	var err error
	if f.Before != nil {
		err = f.Before()
		if err != nil {
			return err
		}
	}
	if f.parent != nil {
		return f.parent.fire_before_func()
	}
	return nil
}

func (f *Flags) fire_after_func(e error) error {
	var err error
	if f.After != nil {
		err = f.After(e)
		if err != nil {
			return err
		}
	}
	if f.parent != nil {
		return f.parent.fire_after_func(e)
	}
	return e
}

func (f *Flags) set_run_func(fc func() error) {
	if f.parent != nil {
		f.parent.set_run_func(fc)
	} else {
		f.runFunc = fc
	}
}

func (f *Flags) selectCommand(arguments []string, chain *[]*Flags) (*Flags, error) {
	if f.registrationErr != nil {
		return nil, f.registrationErr
	}
	*chain = append(*chain, f)
	f.FlagSet.Usage = f.Usage
	// Manual scalar flags may alias a registered config field. Record their typed
	// assignments in argument order, including mixed manual/AutoRegister aliases.
	f.FlagSet.VisitAll(func(option *flag.Flag) {
		if _, bound := option.Value.(*boundValue); bound {
			return
		}
		v := reflect.ValueOf(option.Value)
		if v.Kind() != reflect.Pointer || v.IsNil() || !v.Elem().CanSet() {
			return
		}
		v = v.Elem()
		switch v.Kind() {
		case reflect.String, reflect.Bool, reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Float64:
			option.Value = &recordingValue{Value: option.Value, target: v, owner: f}
		}
	})
	f.collecting = true
	err := f.FlagSet.Parse(arguments)
	f.collecting = false
	if err != nil {
		return nil, err
	}
	arg0 := f.Arg(0)
	for _, c := range f.subs {
		if c.Name() == arg0 {
			return c.selectCommand(f.Args()[1:], chain)
		}
	}
	return f, nil
}

func (f *Flags) Usage() {
	f.usage()
	fmt.Fprintf(os.Stderr, "%s\n", f.help)
}

func (f *Flags) usage() {
	f.help = ""
	f.SetOutput(f)
	f.FlagSet.PrintDefaults()
	if f.help != "" {
		f.help = fmt.Sprintf("%s:\n  %s\n\nOptions:\n", f.str(), f.des) + f.help
	} else {
		f.help = fmt.Sprintf("%s:\n  %s", f.str(), f.des) + f.help
	}
	if len(f.subs) > 0 {
		fmt.Fprint(f, "\nCommands:\n")
		for _, c := range f.subs {
			fmt.Fprintf(f, "  %s:  %s\n", c.Name(), c.des)
		}
	}
}

func (f *Flags) Write(p []byte) (n int, err error) {
	f.help += string(p)
	return len(p), nil
}

func (f *Flags) SubCommand(name, des string) *Flags {
	s := New(name, des)
	s.depth = f.depth + 1
	f.subs = append(f.subs, s)
	s.parent = f
	return s
}

// DumpCfg 将 cfg 覆盖写入 path，按扩展名选择序列化协议：.json → JSON，其余 → YAML。
func DumpCfg(path string, cfg interface{}) error {
	var body []byte
	var err error
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		body, err = json.MarshalIndent(cfg, "", "  ")
	default:
		body, err = yaml.Marshal(cfg)
	}
	if err != nil {
		return err
	}
	f, err := utils.MkFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(body)
	return err
}
