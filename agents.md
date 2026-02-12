# Vigo 后端开发规范 (Agent 专用)

你是一个专业的后端开发 Agent，精通使用 Golang (vigo 框架) 构建高性能后端服务。你的核心任务是根据用户需求，生成完整、规范、可维护的后端代码。你必须严格遵守所有指定的技术栈、项目结构、代码风格和 API 规范。

## 1. 核心原则

1. **洋葱模型 (Onion Model)**: Vigo 的请求处理遵循严格的洋葱模型流水线。理解 `Before` (前置/进入时执行)、`Handler` (业务逻辑)、`After` (后置/离开时执行) 的执行顺序至关重要。
2. **职责单一**:
   - `Before` 中间件：负责鉴权、参数预处理、上下文注入。
   - `Handler`：仅负责业务逻辑处理，返回数据对象，**不**直接写入响应（除非是特殊流式响应）。
   - `After` 中间件：负责统一响应格式化（如 JSON 包装）、日志记录、清理工作。
3. **层级作用域**: 路由中间件具有层级继承特性。父路由的 `Before` 会在子路由之前执行，父路由的 `After` 会在子路由之后执行。利用这一特性在不同层级（全局、资源组、单个接口）注册对应作用域的中间件。
4. **强类型优先**: 优先使用泛型 Handler 机制进行参数解析和业务处理，减少手动类型断言和解析代码。框架会自动识别 `func(*vigo.X, *Req) (Resp, error)` 类型的函数并进行参数解析。

## 2. 项目结构规范

严格按照以下结构组织代码：

```
/
├── api/                # API 接口实现
│   ├── init.go         # 根路由定义 (api.Router)，注册全局中间件 (如日志、CORS，鉴权, 结果统一写入，错误处理等)，集成子资源路由
│   └── {resource}/     # 资源目录 (例如: user, product)
│       ├── init.go     # 资源路由定义 (var Router = vigo.NewRouter())，注册该资源特有的中间件
│       ├── get.go      # 单个资源查询
│       ├── list.go     # 列表查询
│       ├── create.go   # 创建资源
│       ├── patch.go    # 更新资源
│       └── del.go      # 删除资源
├── models/             # 数据模型 (GORM)
│   ├── init.go         # 基础模型定义 (BaseModel)，全局初始化
│   └── {resource}.go   # 资源模型定义
├── cfg/                # 配置与基础设施
│   ├── config.go       # 全局配置
│   └── db.go           # 数据库连接
├── libs/               # 业务通用逻辑库
├── cli/main.go         # 程序入口
├── api.md              # API 文档
└── init.go             # 项目根路由集成
```

## 3. 路由注册与语法

### 3.1 路由匹配语法

Vigo 支持强大的路由匹配语法，**必须**使用以下标准形式：

- **静态路径**: `/users/list`
- **命名参数**: `/{id}` 或 `/{name}` (匹配单个路径段)
- **正则约束**: `/{id:[0-9]+}` (仅匹配数字)
- **单段通配符**: `/*` (匹配单个路径段)
- **递归通配符**: `/**` 或 `/{path:*}` (匹配剩余所有路径段)
- **复合匹配**: `/{file}.{ext}` (匹配 `data.json`，需结合正则使用)

### 3.2 路由注册方法

支持 `Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`, `Any` 等方法。

```go
// api/user/init.go
package user

import (
    "github.com/veypi/vigo"
    "MyProject/libs/middleware"
)

var Router = vigo.NewRouter()

func init() {
    // 1. 注册中间件
    Router.Use(middleware.AuthRequired)     // 前置中间件 (Before)
    Router.After(middleware.JsonResponse)   // 后置中间件 (After)

    // 2. 注册路由
    // 自动解析 getUser 的参数，结果将由父级中间件处理
    // 第2个参数为API描述(string)或参数结构体(struct实例)，可选
    Router.Get("/{id}", "获取用户详情", getUser)

    // 复杂路由示例
    Router.Get("/files/{path:*}", "下载文件", downloadFile)
}
```

### 3.3 子路由扩展 (Extend)

在 `api/init.go` 中，使用 `Extend` 方法将各资源模块的路由挂载到主路由上。

```go
// api/init.go
import (
    "MyProject/api/user"
)

func init() {
    // 将 user.Router 挂载到 /users 路径下
    Router.Extend("/users", user.Router)
}
```

## 4. 参数解析规范

推荐直接使用带有参数结构体的 Handler，框架会自动进行解析。

### 4.1 标签语法

格式: `src:"source[@alias]"`

- `path`: 路径参数 (默认匹配同名字段，可指定 `@alias`)
- `query`: URL 查询参数
- `header`: 请求头
- `json`: JSON 请求体 (默认)
- `form`: 表单数据 (支持 `application/x-www-form-urlencoded` 和 `multipart/form-data`)

**注意**:

- **必填项规则**:
  - **非指针类型**（如 `string`, `int`）默认为**必填**。如果请求中缺少该参数，解析会失败并返回 `409 Invalid Arg` 错误。（注：空值如 `?name=` 被视为参数存在，是合法的）
  - **指针类型**（如 `*string`, `*int`）为**可选**。如果请求中缺少该参数，字段值为 `nil`。
- `default` 标签可设置默认值，但仅对**非指针**和**非 JSON**字段有效。
- `desc`: 参数描述 (用于生成文档)
- `json` 标签用于指定 JSON 字段名。

### 4.2 文件上传与复杂结构体示例

```go
type UserUpdateReq struct {
    // 路径参数: /users/{user_id}
    UserID string `src:"path@user_id" desc:"用户ID"`

    // Header 参数
    TraceID string `src:"header@X-Trace-ID" desc:"链路追踪ID"`

    // Query 参数 (指针表示可选)
    Verbose *bool `src:"query" default:"false" desc:"是否显示详细信息"`

    // JSON Body 参数
    Name  string `json:"name" src:"json" desc:"用户名"`
    Email string `json:"email" src:"json" desc:"邮箱"`

    // 文件上传 (需配合 src:"form")
    Avatar *multipart.FileHeader   `src:"form" desc:"头像文件"` // 单个文件
    Images []*multipart.FileHeader `src:"form" desc:"图片列表"` // 多个文件
}
```

## 5. Context (\*vigo.X) 能力

`*vigo.X` 是请求上下文的核心，提供了丰富的方法：

- **基本属性**: `x.Request` (原生 Request), `x.ResponseWriter()`
- **流程控制**:
  - `x.Next()`: 执行下一个 Handler
  - `x.Stop()`: 停止执行后续 Handler
  - `x.Skip(n)`: 跳过后续 n 个 Handler
- **上下文数据**:
  - `x.Set(key, val)`: 设置上下文变量
  - `x.Get(key)`: 获取上下文变量
- **响应辅助**:
  - `x.JSON(data)`: 发送 JSON 响应
  - `x.WriteHeader(code)`: 发送状态码
- **工具方法**:
  - `x.GetRemoteIP()`: 获取客户端 IP
  - `x.PathParams.Get(key)`: 获取路径参数

## 6. Handlers 与中间件机制

### 6.1 执行流程

`Parent Before` -> `Current Before` -> **`Handler`** -> `Current After` -> `Parent After`

### 6.2 泛型 Handler (推荐)

框架会自动将 `func(*vigo.X, *Req) (Resp, error)` 转换为标准中间件,并根据结构体字段生成输入输出文档。

```go
// 业务 Handler
func CreateUser(x *vigo.X, req *CreateReq) (*User, error) {
    // ... 业务逻辑 ...
    if err != nil {
        return nil, err
    }
    return newUser, nil
}
// List 请求参数
type ListReq struct {
    Page  int    `src:"query" default:"1"`
    Size  int    `src:"query" default:"20"`
    Sort  string `src:"query"`
    Query string `src:"query"`  // 模糊搜索
}
func ListUsers(x *vigo.X, req *ListReq) ([]*User, error) {
    // ... 业务逻辑 ...
}
```

### 6.3 跳过机制

对于某些不需要父级鉴权的接口（如登录），使用 `vigo.SkipBefore`。

```go
// 跳过所有父级 Before 中间件
Router.Post("/login", vigo.SkipBefore, LoginHandler)
```

### 6.4 中间件编写示例与标准库

**前置中间件 (Before)**:
用于鉴权、上下文注入等。

```go
func AuthMiddleware(x *vigo.X) error {
    token := x.Request.Header.Get("Authorization")
    if token == "" {
        return vigo.NewError("Unauthorized").WithCode(401)
    }
    x.Set("user_id", "123")
    return nil // 继续执行
}
```

**后置中间件 (After)**:
用于处理 Handler 返回的数据或错误。推荐直接使用 `github.com/veypi/vigo/contrib/common` 提供的标准中间件。

```go
import "github.com/veypi/vigo/contrib/common"

// 注册标准 JSON 响应处理 (处理正常返回值和错误)
Router.After(common.JsonResponse, common.JsonErrorResponse)
```

如果需要自定义，可参考标准实现：

```go
// 成功响应处理: 接收 Handler 的返回值 data
func JsonResponse(x *vigo.X, data any) error {
    x.WriteHeader(200)
    return x.JSON(data)
}

// 错误响应处理: 接收 Handler 返回的 error
func JsonErrorResponse(x *vigo.X, err error) error {
    code := 400
    if e, ok := err.(*vigo.Error); ok {
        code = e.Code
        // 处理 HTTP 状态码逻辑...
    }
    x.WriteHeader(code)
    // 返回标准错误 JSON
    x.Write(fmt.Appendf([]byte{}, `{"code":%d,"message":"%s"}`, code, err.Error()))
    return nil
}
```

## 7. 错误处理

Handler 返回的 error 会被框架捕获并传递给错误处理中间件。

### 7.1 使用预定义错误 (推荐)

`vigo` 提供了一系列常用的预定义错误，可以直接使用：

```go
// 常用预定义错误
return nil, vigo.ErrNotFound           // 404 Not Found
return nil, vigo.ErrNotAuthorized      // 40101 Not Authorized
return nil, vigo.ErrForbidden          // 403 Forbidden
return nil, vigo.ErrArgInvalid.WithArgs("param_name") // 409 Invalid Arg
return nil, vigo.ErrInternalServer     // 500 Internal Server Error
```

### 7.2 自定义错误

如果预定义错误不满足需求，可以创建新的错误：

```go
// 抛出自定义错误 (带状态码)
return nil, vigo.NewError("用户积分不足").WithCode(400)

// 包装底层错误
return nil, vigo.NewError("数据库查询失败").WithError(err)
```

## 8. 数据库操作 (GORM)

### 8.1 基础模型与迁移

推荐使用 `vigo.Model` 作为基础模型，它提供了标准的 UUID 主键和时间戳字段。

```go
type Model struct {
 ID        string         `json:"id" gorm:"primaryKey;type:varchar(36);comment:ID"`
 CreatedAt time.Time      `json:"created_at"`
 UpdatedAt time.Time      `json:"updated_at"`
 DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
```

同时，使用 `vigo.ModelList` 来统一管理模型的自动迁移。

```go
type ModelList struct {
 list []any
}
// 添加单个模型
func (ms *ModelList) Add(model any)
// 获取所有模型列表
func (ms *ModelList) GetList() []any
// 批量添加模型
func (ms *ModelList) Append(models ...any)
// 自动迁移所有模型 (通常在 clis/main.go 中调用)
func (ms *ModelList) AutoMigrate(db *gorm.DB) error
// 交互式删除所有模型表 (开发环境慎用)
func (ms *ModelList) AutoDrop(db *gorm.DB) error
```

### 8.2 常用操作

- 使用 `cfg.DB()` 获取数据库实例。
- 利用 GORM 的链式操作。

```go
var user models.User
if err := cfg.DB().Where("id = ?", req.ID).First(&user).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, vigo.NewError("未找到记录").WithCode(404)
    }
    return nil, vigo.NewError("系统错误").WithError(err)
}
```

## 9. 常用中间库

## common - 通用工具

```go
r.After(common.JsonResponse, common.JsonErrorResponse)
// common.JsonResponse(x, data)
// common.JsonErrorResponse(x, err)
```

## config - 配置管理

### AES 加密

```go
import "github.com/veypi/vigo/contrib/config"

key := config.Key("your-secret-key")

// 加密
encrypted, err := key.Encrypt("sensitive data")

// 解密
decrypted, err := key.Decrypt(encrypted)
```

### Redis

```go
import "github.com/veypi/vigo/contrib/config"

// 内存模式 (测试用)
redis := &config.Redis{Addr: "memory"}
client := redis.Client()

// 真实 Redis
redis := &config.Redis{
    Addr:     "localhost:6379",
    Password: "password",
    DB:       0,
}
client := redis.Client()
```
