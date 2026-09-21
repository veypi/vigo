# 配置与命令行

`flags` 统一处理四层配置，优先级固定为：

**命令行 > 环境变量 > 配置文件 > 默认值。**

```go
type Options struct {
    Port   int  `json:"port" yaml:"port" default:"8080"`
    Reload bool `json:"reload" yaml:"reload" default:"true"`
}

options := &Options{}
cmd := flags.New("app", "example")
cmd.AutoRegister(options)
cmd.ConfigFile("config.yaml")
cmd.Command = func() error { return run(options) }

if err := cmd.Parse(); err != nil {
    // flag.ErrHelp 表示正常请求帮助；其余错误由应用决定如何报告及退出。
    return err
}
return cmd.Run()
```

需要 `-f` 选择配置文件时，用 `cmd.ConfigFileFlag("f", "config.yaml")`
代替 `ConfigFile`。文件路径在常规参数解析中确定，应用不需要预解析或重复调用 `Parse`。

## 各入口的职责

| 入口 | 行为 |
| --- | --- |
| `AutoRegister(cfgs...)` | 只声明 flag 和 env 绑定，不修改 cfg，也不读取文件或环境变量；可一次传入多个结构体（共享配置 + 命令私有选项） |
| `Parse()` / `ParseArgs(args)` | 解析一次命令行，按优先级应用全部配置层，返回错误，不调用 `os.Exit` |
| `LoadCfg(path, cfg)` | 只应用默认值和文件，供设置页读取持久配置；返回可忽略的诊断信息 |
| `SetDefaults(cfg)` | 从 `default` 标签补充未设置字段，供创建默认配置实例使用 |
| `DumpCfg(path, cfg)` | 保存明确传入的配置，不混入其他来源 |
| `ConfigIssues()` | 返回本次 Parse 忽略的配置问题，包含来源、字段、原因，不包含原始值 |

`default` 标签和应用预先初始化的非零字段属于默认层；没有默认值的字段使用 Go 零值。
默认层只在加载其他来源之前应用。文件或环境变量中的 `false`、`0`、`""`、`[]` 和 `{}`
都是明确提供的值，不会被默认值覆盖。不要先调用 `LoadCfg` 再通过 `AutoRegister` 启动；
启动统一声明 `ConfigFile`，文件读取与默认值应用由 Parse 安排。

## 字段与容错

- flag 名来自 `json` 标签，env 名是对应大写名称；嵌套字段使用 `db.port` / `DB_PORT`。
- JSON 文件遵循 `json` 标签，YAML 文件遵循 `yaml` 标签及各格式原有的默认命名。
- `.json` 扩展名使用 JSON，其余使用 YAML；扩展名不区分大小写。
- 未知字段忽略，单个字段类型错误保留该字段的低优先级有效值。
- 嵌套结构体按字段合并，保留已有指针身份；集合及自定义解码器成功后才整体应用。
- 文件缺失、无法读取或语法损坏不阻断启动；环境变量和命令行仍可生效。
- 无效环境变量不覆盖文件或默认值；错误的显式命令行参数返回用法错误。
- 读取不自动改写文件，不打印告警；调用方可按需展示诊断。
- 父命令注册的 flag 对整条子命令链持续有效，写在子命令后面同样解析；子命令只需注册自己额外的参数，同名 flag 覆盖继承值。
- 子命令复用父级配置时不会重新覆盖父命令的显式参数，长短别名按出现顺序生效。

列表、数组、map 参数和环境变量可使用 JSON 文本或 JSON 文件路径。
`time.Duration`、`time.Time`、自定义标量类型和文本解码器由库统一转换。
权限规则、凭证有效性等业务语义由应用在解析后校验，业务代码不需要再解析 YAML/JSON。
