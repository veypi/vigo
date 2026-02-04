# Vigo 插件开发规范 (Agent 指南)

本文档旨在指导开发者（及 AI Agent）如何编写符合 Vigo 插件加载器安全规范的插件代码。

## 1. 插件基本结构

一个合法的 Vigo 插件必须满足以下基本结构要求：

1.  **包名**: 必须是 `package main`。
2.  **导出 Router**: 必须导出一个名为 `Router` 的公共变量，类型为 `*vigo.Router`（通常通过 `vigo.NewRouter()` 创建）。
3.  **可选 Init**: 可以导出一个名为 `Init` 的函数 `func Init() error`，用于初始化逻辑。

### 标准模板

```go
package main

import (
    "github.com/veypi/vigo"
)

// 必须: 导出 Router 变量
var Router = vigo.NewRouter()

// 可选: 初始化函数
func Init() error {
    // 可以在这里做一些初始化工作
    return nil
}

func init() {
    // 注册路由
    Router.Get("/hello", func(x *vigo.X) error {
        return x.JSON(map[string]string{"message": "Hello from Plugin!"})
    })
}
```

### 进阶模板：泛型 Handler 与数据库操作

利用 Vigo 的泛型 Handler 机制，可以自动解析请求参数（Path, Query, JSON 等），并返回强类型对象。

```go
package main

import (
    "github.com/veypi/vigo"
    "gorm.io/gorm"
)

var Router = vigo.NewRouter()

// 定义数据模型
type Demo struct {
    ID   uint   `json:"id" gorm:"primaryKey"`
    Name string `json:"name"`
}

// 定义请求参数结构
type UpdateDemoReq struct {
    ID   uint   `src:"path"`          // 自动解析路径参数 /demo/{id}
    Name string `json:"name" src:"json"` // 自动解析 JSON Body
}

// 业务处理函数
// 签名符合: func(*vigo.X, *Req) (*Resp, error)
// 框架会自动进行参数绑定、验证和错误处理
func updateDemo(x *vigo.X, req *UpdateDemoReq) (*Demo, error) {
    // 获取数据库连接 (假设主程序已将 DB 注入到 Context)
    // 注意: 插件禁止直接调用 gorm.Open，必须使用主程序提供的连接
    val := x.Get("DB")
    if val == nil {
        return nil, vigo.NewError("database not available")
    }
    db, ok := val.(*gorm.DB)
    if !ok {
        return nil, vigo.NewError("invalid database connection")
    }

    // 业务逻辑: 查询 -> 更新 -> 返回
    var demo Demo
    if err := db.First(&demo, req.ID).Error; err != nil {
        return nil, err // 返回 error，框架会统一处理为错误响应
    }

    demo.Name = req.Name
    if err := db.Save(&demo).Error; err != nil {
        return nil, err
    }

    return &demo, nil // 返回结构体指针，框架会将其序列化为 JSON 响应
}

func init() {
    // 注册泛型 Handler
    // 路由定义中的 {id} 会自动映射到 UpdateDemoReq.ID
    Router.Patch("/demo/{id}", updateDemo)
}
```

## 2. 安全限制与规范

为了保证主程序的安全，插件代码受到严格的静态分析和运行时限制。违反以下规则将导致加载失败。

### 2.1 导入限制 (Import Restrictions)

*   **允许的包**: 仅允许导入标准库（如 `fmt`, `strings`, `time` 等）和 `github.com/veypi/vigo` 核心库。
*   **禁止的包**:
    *   **禁止**导入 `github.com/veypi/vigo/contrib` 及其子包。
    *   **禁止**导入 `unsafe` 包。
    *   **禁止**导入未在白名单中的第三方库。
*   **禁止别名**:
    *   ❌ `import f "fmt"` (禁止使用别名)
    *   ❌ `import . "fmt"` (禁止使用点导入)
    *   ✅ `import "fmt"` (必须直接导入)

### 2.2 函数调用限制 (Forbidden Functions)

为了防止插件破坏全局状态或执行危险操作，以下函数调用被**明确禁止**：

*   **vigo.New()**: 禁止创建新的 Vigo Application 实例。插件只能挂载到现有的 Router 上。
    *   ❌ `app := vigo.New()`
*   **gorm.Open()**: 禁止直接建立数据库连接。如果需要数据库访问，应通过主程序提供的上下文或接口进行（具体取决于主程序暴露的能力）。
    *   ❌ `db, err := gorm.Open(...)`

### 2.3 行为规范

*   **路由挂载**: 插件中的路由是相对路由。例如，如果插件被挂载在 `/plugin/a`，而插件内注册了 `/hello`，则最终访问路径为 `/plugin/a/hello`。
*   **资源清理**: 尽量避免启动永不退出的 Goroutine，除非你有把握在插件卸载时（如果支持）能够清理它们。

## 3. 代码自检清单

在提交插件代码前，请检查：

- [ ] 包名是否为 `package main`？
- [ ] 是否导出了 `var Router`？
- [ ] 是否使用了禁止的包（如 `vigo/contrib`）？
- [ ] 是否使用了禁止的函数（如 `vigo.New`）？
- [ ] 是否使用了 import 别名（如 `import v "..."`）？

## 4. 错误示例

```go
// ❌ 错误示例
package main

import (
    v "github.com/veypi/vigo"           // 错误: 禁止别名
    "github.com/veypi/vigo/contrib/log" // 错误: 禁止导入 contrib
)

func init() {
    app := v.New() // 错误: 禁止调用 vigo.New
    // ...
}
```

## 5. 插件测试指南

推荐使用 `vigo/contrib/plugin` 提供的 `NewTestHelper` 来编写原生 Go 测试，避免编写复杂的 Shell 脚本。

### 5.1 TestHelper 特性

*   **自动中间件注册**: `NewTestHelper` 创建的 Router 默认已注册 `common.JsonResponse` 和 `common.JsonErrorResponse` 后置中间件。这意味着你的测试环境与生产环境一致，Handler 返回的 `struct` 或 `error` 会被自动序列化为 JSON 响应。
*   **依赖自动替换**: 能够自动识别本地 Vigo 项目根目录，并配置 `go.mod` replace 规则，方便本地开发调试。

### 5.2 测试代码示例

建议每个插件文件（如 `main.go`）对应一个测试文件（如 `main_test.go`）。

```go
package main

import (
	"path/filepath"
	"os"
	"strings"
	"testing"

	"github.com/veypi/vigo/contrib/plugin"
)

func TestMyPlugin(t *testing.T) {
	// 1. 初始化测试辅助工具
	// 此时 Router 已配置好 JSON 响应处理中间件
	helper := plugin.NewTestHelper(t)

	// 2. 加载插件
	// 方式 A: 直接加载文件 (推荐用于 main.go 测试)
	wd, _ := os.Getwd()
	pluginPath := filepath.Join(wd, "main.go")
	if err := helper.Loader.Load(helper.Router, "/plugin", pluginPath); err != nil {
		t.Fatalf("Failed to load plugin: %v", err)
	}

	// 方式 B: 直接加载源码字符串 (推荐用于快速测试或异常用例)
	// helper.Loader.LoadSource(helper.Router, "/plugin", []byte(sourceCode))

	// 3. 发起请求并验证
	// 假设插件注册了 /hello
	w := helper.Request("GET", "/plugin/hello", nil)
	
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Hello from Plugin!") {
		t.Errorf("Expected response containing 'Hello from Plugin!', got %s", w.Body.String())
	}
}
```
