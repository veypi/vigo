# Vigo

[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.24-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-Apache%202.0-green)](LICENSE)

Vigo 是一个高性能、简洁易用的 Go Web 框架，专为构建现代 RESTful API 而设计。它提供了强大的路由系统、智能参数解析、灵活的中间件机制。

## 🚀 特性

- **高性能路由系统** - 基于 Radix Tree 和零分配（Zero-Allocation）设计，支持有序匹配、回溯和优先级控制
- **灵活的路由语法** - 支持 `{param}`、`{path:*}`、`**`、正则约束 `{id:[0-9]+}` 以及复合匹配 `{file}.{ext}`
- **智能参数解析** - 自动从 Path、Query、Header、JSON、Form 等多种来源解析参数到结构体
- **类型安全** - 强类型的参数解析和验证，减少运行时错误
- **中间件机制** - 支持全局、路由组和单个路由级别的中间件（Use/After）
- **生产就绪** - 支持 SSE、文件服务、优雅关闭等特性

## 📦 安装

```bash
go mod init your-project
go get github.com/veypi/vigo
```

## 🏁 快速开始

```go
package main

import (
    "github.com/veypi/vigo"
    "github.com/veypi/vigo/logv"
)

func main() {
    // 创建应用
    app, err := vigo.New()
    if err != nil {
        logv.Fatal().Err(err).Msg("Failed to create app")
    }

    // 注册路由
    router := app.Router()
    
    // 基础路由
    router.Get("/hello", hello)
    
    // 带参数的路由
    router.Get("/user/{id}", getUser)
    
    // 正则约束路由
    router.Get("/files/{file:[a-z]+}.{ext}", getFile)

    // 启动服务
    logv.Info().Msg("Starting server on :8000")
    if err := app.Run(); err != nil {
        logv.Fatal().Err(err).Msg("Server failed")
    }
}

func hello(x *vigo.X) (any, error) {
    return map[string]string{"message": "Hello, Vigo!"}, nil
}

type getUserOpts struct {
    ID string `json:"id" src:"path"`
}

func getUser(x *vigo.X) (any, error) {
    args := &getUserOpts{}
    if err := x.Parse(args); err != nil {
        return nil, err
    }
    
    return map[string]any{
        "id":   args.ID,
        "name": "User " + args.ID,
    }, nil
}

func getFile(x *vigo.X) {
    file := x.PathParams.Get("file")
    ext := x.PathParams.Get("ext")
    x.String(200, "File: %s, Ext: %s", file, ext)
}
```

## 🛣️ 路由语法详解

Vigo 采用了全新的路由引擎，支持丰富且直观的匹配规则：

### 1. 静态路由
最普通的路径匹配。
```go
router.Get("/users/list", handler)
```

### 2. 命名参数 `{param}`
匹配单个路径段，参数值可以通过 `x.PathParams.Get("id")` 获取，或通过结构体 `src:"path"` 标签自动解析。
```go
router.Get("/users/{id}", handler)
```

### 3. 参数解析
Vigo 支持将 HTTP 请求参数自动解析到 Go 结构体中。

**标签语法**: `src:"source[@alias]"`

- `src:"path"`: 路径参数 (默认匹配同名字段，可指定 `@alias`)
- `src:"query"`: URL 查询参数
- `src:"header"`: 请求头
- `src:"form"`: 表单数据 (支持 `application/x-www-form-urlencoded` 和 `multipart/form-data`)
- `src:"json"`: JSON 请求体 (默认)

**其他标签**:
- `default`: 设置默认值 (仅限非指针/非JSON字段)
- `json`: 指定 JSON 字段名

**示例**:
- `src:"query@page_size"`: 映射 URL 参数 `?page_size=10` 到结构体字段
- `src:"header@X-User-ID"`: 映射请求头 `X-User-ID` 到结构体字段

**必填项规则**：
- **非指针类型**（如 `string`, `int`）默认为**必填**。如果请求中缺少该参数，解析会失败并返回 `409 Invalid Arg` 错误。（注：空值如 `?name=` 被视为参数存在，是合法的）
- **指针类型**（如 `*string`, `*int`）为**可选**。如果请求中缺少该参数，字段值为 `nil`。
- 使用 `default` 标签可以为必填参数提供默认值。

```go
type UserReq struct {
    ID    string  `src:"path"`                 // 必填，路径参数
    Name  string  `src:"query"`                // 必填，缺少则报错（空字符串合法）
    Age   *int    `src:"query"`                // 可选，缺少则为 nil
    Role  string  `src:"query" default:"user"` // 必填但有默认值
    Token string  `src:"header@X-Auth-Token"`  // 从 Header 中获取 X-Auth-Token
    Page  int     `src:"query@p"`              // 从 Query 中获取 p 参数 (如 ?p=1)
}
```

### 4. 通配符 `{path:*}` 或 `*`
匹配当前段及其之后的所有内容（非贪婪，除非是最后一个节点）。
```go
router.Get("/static/{filepath:*}", handler)
// 或简写
router.Get("/static/*", handler)
```

### 5. 递归通配符 `**`
匹配剩余所有路径，通常用于 SPA 前端路由兜底或文件服务。
```go
router.Get("/assets/**", handler)
```

### 6. 正则约束 `{name:regex}`
只有当路径段满足正则表达式时才匹配。
```go
// 仅匹配数字 ID
router.Get("/users/{id:[0-9]+}", handler)
```

### 7. 复合匹配 `{a}.{b}`
在一个路径段内匹配多个参数，支持前缀、后缀和中缀匹配。
```go
// 匹配如 "style.css", "script.js"
router.Get("/static/{name}.{ext}", handler)

// 匹配如 "v1-api", "v2-api"
router.Get("/{version}-api", handler)
```

## ⛓️ 处理流水线 (Handler Pipeline)

Vigo 的请求处理采用洋葱模型（Onion Model）构建的流水线。

### 1. 执行顺序
对于一个特定路由，处理链由以下部分组成，并按顺序执行：
1. **父路由 Before 中间件** (从根路由向下)
2. **当前路由 Before 中间件**
3. **路由处理函数** (Set/Get/Post 等注册的 handler)
4. **当前路由 After 中间件**
5. **父路由 After 中间件** (从当前路由向上)

### 2. Handler 定义
Vigo 支持极其灵活的 Handler 函数签名，你可以根据需要选择最适合的形式：

- **标准形式**: `func(*vigo.X)`
- **带错误返回**: `func(*vigo.X) error` (返回 error 会中断流水线并触发错误处理)
- **标准 HTTP**: `func(http.ResponseWriter, *http.Request)`
- **管道模式**: `func(*vigo.X, any)` (接收 `x.PipeValue`，可用于在中间件间传递数据)
- **错误处理**: `func(*vigo.X, error) error` (仅在发生错误时执行)

所有支持的签名：
- `func(*X)`
- `func(*X) error`
- `func(*X) any`
- `func(*X) (any, error)`
- `func(*X, any)`
- `func(*X, any) any`
- `func(*X, any) error`
- `func(*X, any) (any, error)`
- `func(http.ResponseWriter, *http.Request)`
- `func(http.ResponseWriter, *http.Request) error`

### 3. 高级用法

#### 3.1 跳过前置中间件
使用 `vigo.SkipBefore` 可以让当前路由跳过所有父级路由定义的 `Before` 中间件（但保留 `After` 中间件）。这在某些无需鉴权或需要特殊处理的接口（如登录、公开资源）非常有用。

```go
// 登录接口跳过鉴权中间件
router.Get("/login", vigo.SkipBefore, loginHandler)
```

#### 3.2 强类型 Handler
Vigo 支持直接注册强类型的 Handler，框架会自动解析请求参数并处理响应，无需手动调用辅助函数。

```go
// 定义请求结构体
type UserReq struct {
    Name string `json:"name"`
}

// 定义响应结构体
type UserResp struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}

// 强类型 Handler
func CreateUser(x *vigo.X, req *UserReq) (*UserResp, error) {
    // req 已经被自动解析填充
    return &UserResp{ID: "1", Name: req.Name}, nil
}

// 注册路由
router.Post("/users", CreateUser)
```

### 4. 控制流
- **自动执行**: 默认情况下，流水线中的 Handler 会自动顺序执行。
- **x.Next()**: 在中间件中调用 `x.Next()` 可以显式执行后续 Handler，并在其返回后继续执行当前中间件的剩余逻辑（用于后置处理，如计算耗时）。
- **x.Stop()**: 停止流水线，后续 Handler 不再执行。
- **返回 error**: 停止流水线，并将 error 传递给后续的 `FuncErr` 类型的 Handler 进行处理。

## 📝 技术栈约束

- **框架**: vigo (github.com/veypi/vigo)
- **语言**: Golang 1.24+

