# Vigo 使用指南

## 快速开始

```go
package main

import (
    "github.com/veypi/vigo"
)

func main() {
    app, err := vigo.New()
    if err != nil {
        panic(err)
    }

    app.Router().Get("/", func(x *vigo.X) (any, error) {
        return "Hello, World!", nil
    })

    app.Run()
}
```

## 核心类型

```go
// Handler 函数签名
type FuncX2AnyErr = func(*X) (any, error)
type FuncErr = func(*X, error) error
type FuncStandard[T, U any] func(*X, T) (U, error)

// 简写类型
type M = map[string]any
type S = []any
```

## 创建应用

```go
// 默认配置
app, _ := vigo.New()

// 自定义配置
app, _ := vigo.New(
    vigo.WithHost("0.0.0.0"),
    vigo.WithPort(8080),
    vigo.WithDocPath("_api"),
    vigo.WithLoggerPath("./logs"),
    vigo.WithLoggerLevel("debug"),
    vigo.WithPrettyLog(),
)
```

## 路由

### 基础路由

```go
r := app.Router()

r.Get("/users", handler)
r.Post("/users", handler)
r.Put("/users/{id}", handler)
r.Patch("/users/{id}", handler)
r.Delete("/users/{id}", handler)
r.Head("/resource", handler)
r.Any("/any", handler)
```

### 路由模式

```go
// 静态路由
r.Get("/api/users", handler)

// 参数路由
r.Get("/api/users/{id}", handler)

// 通配符路由
r.Get("/files/*", handler)           // 匹配单个段
r.Get("/files/{path:*}", handler)    // 命名通配符
r.Get("/static/**", handler)         // 匹配所有剩余路径

// 正则路由
r.Get("/api/users/{id:[0-9]+}", handler)
r.Get("/api/{name:[a-z]+}.{ext:json|xml}", handler)
```

### 子路由

```go
api := r.SubRouter("/api/v1")
api.Get("/users", listUsers)      // 最终路由: /api/v1/users
api.Get("/posts", listPosts)

// 多域名路由
sub := app.Domain("api.example.com")
sub.Get("/users", handler)

// 泛域名
wildcard := app.Domain("*.example.com")
```

### 中间件

```go
// Use: 前置中间件
r.Use(authMiddleware)

// After: 后置中间件
r.After(logMiddleware)

// 忽略前置中间件
r.Use(vigo.SkipBefore)
```

### 路由变量

```go
r.SetVar("key", "value")

// 在 handler 中获取
value := x.Get("key")
```

## 上下文 X

### 控制流

```go
func handler(x *vigo.X) (any, error) {
    x.Next()          // 执行下一个 handler
    x.Stop()          // 停止执行后续 handler
    x.Skip(2)         // 跳过 n 个后续 handler
    return nil, nil
}
```

### 参数获取

```go
// URL 路径参数 /users/{id}
id := x.PathParams.Get("id")

// 查询参数 /search?q=hello
q := x.Request.URL.Query().Get("q")

// Header
auth := x.Request.Header.Get("Authorization")

// 远程 IP
ip := x.GetRemoteIP()
```

### 参数解析

```go
type UserReq struct {
    ID       string                `src:"path@user_id" json:"id"`
    Name     string                `src:"form" json:"name" default:"guest"`
    Age      int                   `src:"query" json:"age"`
    Token    string                `src:"header" json:"token"`
    Email    string                `src:"json" json:"email"`
    Avatar   *multipart.FileHeader `src:"form" json:"avatar"`
}

func handler(x *vigo.X, req *UserReq) (*User, error) {
    // 自动解析到结构体
    return &User{Name: req.Name}, nil
}
```

### 响应写入

```go
func handler(x *vigo.X) (any, error) {
    // JSON
    x.JSON(map[string]string{"msg": "ok"})

    // String
    x.String(200, "Hello %s", "World")
    x.WriteString("Hello")

    // HTML 模板
    x.HTMLTemplate("<h1>{{.Name}}</h1>", map[string]string{"Name": "Vigo"})

    // 文件
    x.File("/path/to/file.pdf")

    // Embed 文件
    x.Embed(&embedFS, "static/index.html")

    return nil, nil
}
```

### SSE

```go
func sseHandler(x *vigo.X) (any, error) {
    writer := x.SSEWriter()
    writer([]byte("data: hello\n\n"))

    // 或使用 Event
    event := x.SSEEvent()
    event("message", `{"msg": "hello"}`)

    return nil, nil
}
```

### 上下文值

```go
// 设置
x.Set("user_id", "123")

// 获取
userID := x.Get("user_id")

// 获取原生 context
ctx := x.Context()
```

## Handler 签名支持

```go
// 标准签名
func handler(x *vigo.X) (any, error)

// 只返回错误
func handler(x *vigo.X) error

// 自动解析请求体
func handler(x *vigo.X, req *MyRequest) (any, error)
func handler(x *vigo.X, req *MyRequest) (*MyResponse, error)
func handler(x *vigo.X, req *MyRequest) error

// 使用 PipeValue (链式传递)
func handlerA(x *vigo.X) (any, error) {
    x.PipeValue = "data"
    return nil, nil
}
func handlerB(x *vigo.X, data any) (any, error) {
    // data == "data"
    return data, nil
}

// HTTP 原生
func handler(w http.ResponseWriter, r *http.Request)

// 错误处理
func errorHandler(x *vigo.X, err error) error {
    x.JSON(map[string]string{"error": err.Error()})
    return nil
}
```

## 错误处理

```go
// 预定义错误
vigo.ErrNotFound           // code: 404
vigo.ErrArgMissing         // code: 409
vigo.ErrArgInvalid         // code: 409
vigo.ErrNotAuthorized      // code: 40101
vigo.ErrNotPermitted       // code: 40102
vigo.ErrForbidden          // code: 403
vigo.ErrInternalServer     // code: 500
vigo.ErrTooManyRequests     // code: 429

// 使用
return nil, vigo.ErrNotFound
return nil, vigo.ErrArgMissing.WithArgs("username")
return nil, vigo.NewError("custom error").WithCode(400)

// 错误链
return nil, vigo.ErrInternalServer.WithError(err)
return nil, vigo.ErrInternalServer.WithMessage("db failed")
return nil, vigo.ErrInternalServer.WithString("details")
```

## API 文档

```go
// 自动启用 (需设置 DocPath)
app, _ := vigo.New(vigo.WithDocPath("_api"))

// 访问
// GET /_api      - HTML 文档页面
// GET /_api.json - JSON 格式文档

// 添加描述
r.Get("/users", "获取用户列表", handler)

// 描述 + 请求/响应结构
r.Post("/users", "创建用户", &CreateUserReq{}, handler)
```

## 数据库

```go
// 模型基类
type User struct {
    vigo.Model              // ID, CreatedAt, UpdatedAt, DeletedAt
    Name     string `json:"name"`
}

// 模型列表
var models vigo.ModelList
models.Add(&User{})
models.Add(&Post{})

// 自动迁移
models.AutoMigrate(db)

// 删除表 (交互式确认)
models.AutoDrop(db)
```

## 扩展库 (Contrib)

### CRUD 自动生成

`github.com/veypi/vigo/contrib/crud` 可以快速生成增删改查接口。

```go
import "github.com/veypi/vigo/contrib/crud"

type Product struct {
    vigo.Model
    Name   string `json:"name"`
    UserID string `json:"user_id"`
}

// 初始化控制器
ctrl := crud.New(&Product{})

// 配置权限控制 (SetFilter)
// 该过滤器会对所有操作 (List, Get, Create, Update, Delete) 生效
ctrl.SetFilter(func(x *vigo.X, db *gorm.DB) (*gorm.DB, error) {
    // 示例：仅允许操作当前用户的数据
    userID := x.Get("user_id")
    if userID == nil {
         // 如果未登录，返回错误以中断操作
         return nil, vigo.ErrNotAuthorized
    }
    // 注意：Where 条件对 Create 操作中的数据赋值无效，仅用于 SQL WHERE 子句
    return db.Where("user_id = ?", userID), nil
}).SetBeforeCreate(func(x *vigo.X, req *User) error {
    // 在创建前强制设置 UserID
    req.UserID = x.Get("user_id").(string)
    return nil
}).SetBeforeUpdate(func(x *vigo.X, data map[string]any) error {
    // 防止更新 user_id 字段（虽然 update 逻辑已自动剔除 id，但其他敏感字段需手动处理）
    delete(data, "user_id")
    return nil
})

// 注册路由
ctrl.Register(r.SubRouter("/products"))
```
