# Vigo Plugin Library

`vigo/contrib/plugin` 是一个用于 Vigo 框架的动态插件加载库。它允许主程序在运行时加载外部的 Go 代码（源码或编译后的 `.so` 文件）作为插件，并将其路由挂载到主程序的路由树上。

该库特别注重安全性，提供了严格的依赖检查、代码静态分析和运行时沙箱机制，适合用于允许用户上传或编写自定义逻辑的场景。

## 功能特性

*   **动态加载**: 支持加载预编译的 `.so` 插件或直接加载 `.go` 源码（自动编译）。
*   **源码支持**: 支持直接传入 Go 源码字符串进行加载。
*   **安全沙箱**:
    *   **导入控制**: 支持允许 (`AllowedPrefixes`) 和禁止 (`ForbiddenPrefixes`) 的包导入列表。
    *   **禁止操作**: 可禁止特定的函数调用（如 `vigo.New`, `gorm.Open` 等敏感操作）。
    *   **别名限制**: 可禁止使用导入别名 (`import f "fmt"`) 和点导入 (`import . "fmt"`).
*   **编译管理**: 可配置编译输出目录，支持 `~` 路径展开。

## 快速开始

### 1. 安装

```go
import "github.com/veypi/vigo/contrib/plugin"
```

### 2. 使用 Loader 加载插件

```go
package main

import (
    "github.com/veypi/vigo"
    "github.com/veypi/vigo/contrib/plugin"
)

func main() {
    app:= vigo.New()

    // 1. 使用默认 Loader (包含严格的安全策略)
    // 默认禁止 vigo/contrib 导入，禁止 vigo.New 等
    loader := plugin.NewLoader()

    // 2. 加载插件
    // 参数: 主路由, 挂载前缀, 插件路径(或源码)
    err := loader.Load(app.Router(), "/my-plugin", "./plugins/my_plugin.go")
    if err != nil {
        panic(err)
    }

    app.Run()
}
```

## 配置说明

`Loader` 结构体提供了丰富的配置项来自定义加载行为：

```go
type Loader struct {
    // 允许导入的包前缀白名单
    AllowedPrefixes []string

    // 禁止导入的包前缀黑名单 (优先级高于白名单)
    // 默认包含 "github.com/veypi/vigo/contrib"
    ForbiddenPrefixes []string

    // 禁止调用的函数/方法
    // Map key 为包路径, value 为函数名列表
    // 默认禁止: gorm.Open, vigo.New
    ForbiddenSelectors map[string][]string

    // 是否允许导入别名 (默认为 false)
    AllowImportAlias bool

    // 插件编译产物存放目录
    // 默认为系统临时目录下的 vigo 子目录 (os.TempDir() + "/vigo")
    CompileDir string

    // 本地依赖替换 (用于 go.mod replace)
    // Map key 为模块路径, value 为本地文件路径
    // 示例: "github.com/veypi/vigo": "/local/path/to/vigo"
    LocalDeps map[string]string
}
```

### 自定义配置示例

```go
l := plugin.NewLoader()

// 允许额外的包
l.AllowedPrefixes = append(l.AllowedPrefixes, "github.com/my/custom/lib")

// 禁止更多包
l.ForbiddenPrefixes = append(l.ForbiddenPrefixes, "unsafe")

// 修改编译目录
l.CompileDir = "/tmp/vigo-plugins"

// 直接加载源码字符串
sourceCode := `
package main
import "github.com/veypi/vigo"
var Router = vigo.NewRouter()
func init() {
    Router.Get("/", func(x *vigo.X) error { return x.JSON("hello") })
}
`
err := l.Load(router, "/dynamic", sourceCode)
```

## 注意事项

1.  **环境一致性**: 插件必须使用与主程序相同的 Go 版本和依赖版本编译。
2.  **编译环境**: 如果加载 `.go` 源码，运行环境必须安装了 `go` 命令行工具。
3.  **安全性**: 默认配置旨在防止插件破坏主程序稳定性（如创建新的 App 实例、随意连接数据库等），但在生产环境使用时，建议根据实际需求进一步收紧 `AllowedPrefixes`。
