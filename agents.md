---
name: vigo-backend
description: "Vigo 框架后端开发规范。当需要使用 Golang + Vigo 框架开发 RESTful API 时调用此技能，包含路由定义、参数解析、中间件、错误处理、数据库操作等完整规范。"
---

# Vigo 后端开发规范

## 1. 核心原则

### 1.1 洋葱模型

请求处理流水线：`Before` → `Handler` → `After`

| 阶段    | 职责                         | 说明                         |
| ------- | ---------------------------- | ---------------------------- |
| Before  | 鉴权、参数预处理、上下文注入 | 进入时执行                   |
| Handler | 业务逻辑                     | 返回数据对象，不直接写入响应 |
| After   | 响应格式化、日志记录、清理   | 离开时执行                   |

### 1.2 层级作用域

父路由 `Before` 先于子路由执行，父路由 `After` 后于子路由执行。

### 1.3 强类型优先

使用泛型 Handler `func(*vigo.X, *Req) (Resp, error)`，框架自动解析参数。

---

## 2. 项目结构

```
/
├── api/
│   ├── init.go          # API根路由，注册全局中间件
│   └── {resource}/
│       ├── init.go      # 资源路由
│       ├── get.go       # 查询单个资源
│       ├── list.go      # 列表查询
│       ├── create.go    # 创建资源
│       ├── patch.go     # 更新资源
│       └── del.go       # 删除资源
├── models/              # GORM 数据模型
│   ├── init.go
│   └── {resource}.go
├── cfg/                 # 配置与基础设施
│   ├── config.go
│   └── db.go
├── libs/                # 业务通用逻辑库
├── cli/main.go          # 程序入口
└── init.go              # 项目根路由集成
```

---

## 3. 路由系统

### 3.1 路由语法

| 语法                 | 说明     | 示例               |
| -------------------- | -------- | ------------------ |
| `/{param}`           | 命名参数 | `/users/{id}`      |
| `/{param:[0-9]+}`    | 正则约束 | `/{id:[0-9]+}`     |
| `/*`                 | 单段通配 | `/files/*`         |
| `/**` 或 `/{path:*}` | 递归通配 | `/static/{path:*}` |

### 3.2 路由注册

```go
// api/user/init.go
var Router = vigo.NewRouter()
func init() {
    // 路由注册
    // Use和After对该层级路由下所有方法和子路由都生效
    Router.Use(PermCheck)
    Router.Get("/{id}", "获取用户详情", getUser)
    // 写在注册函数里的中间件，只对该接口生效
    Router.Post("/login", vigo.SkipBefore, login)  // 跳过父级 Before
    // 定义子路由
    msgRouter := Router.SubRouter("msg")
}
```

### 3.3 子路由挂载

```go
// api/init.go
import "MyProject/api/user"

func init() {
    // 中间件注册
    Router.Use(middleware.AuthRequired)     // Before
    Router.After(common.JsonResponse)       // After
    Router.Extend("/users", user.Router)
}
```

---

## 4. 参数解析

### 4.1 标签语法

格式: `src:"source[@alias]"`

| Source   | 说明         | 必填规则             |
| -------- | ------------ | -------------------- |
| `path`   | 路径参数     | 非指针必填，指针可选 |
| `query`  | URL 查询参数 | 同左                 |
| `header` | 请求头       | 同左                 |
| `json`   | JSON Body    | 同左                 |
| `form`   | 表单数据     | 同左                 |

**必填规则**：非指针类型默认必填，指针类型可选。

**`default` 标签**：设置默认值，仅对非指针类型生效，对 `json`/`form` 源无效。

### 4.2 完整示例

```go
type UserUpdateReq struct {
    // 路径参数: /users/{user_id}
    UserID  string `src:"path@user_id" desc:"用户ID"`

    // Header 参数
    TraceID string `src:"header@X-Trace-ID" desc:"链路追踪ID"`

    // Query 参数 (指针表示可选, 或搭配 default 使非指针变为可选)
    Verbose bool   `src:"query" default:"false" desc:"详细模式"`

    // JSON Body 参数
    Name    string `json:"name" src:"json" desc:"用户名"`
    Email   string `json:"email" src:"json" desc:"邮箱"`

    // 文件上传
    Avatar  *multipart.FileHeader   `src:"form" desc:"头像"`
    Images  []*multipart.FileHeader `src:"form" desc:"图片列表"`
}
```

---

## 5. Context (\*vigo.X) 方法

| 方法                    | 说明                  |
| ----------------------- | --------------------- |
| `x.Next()`              | 执行下一个 Handler    |
| `x.Stop()`              | 停止执行后续 Handler  |
| `x.Skip(n)`             | 跳过后续 n 个 Handler |
| `x.Set(key, val)`       | 设置上下文变量        |
| `x.Get(key)`            | 获取上下文变量        |
| `x.JSON(data)`          | 发送 JSON 响应        |
| `x.WriteHeader(code)`   | 发送状态码            |
| `x.GetRemoteIP()`       | 获取客户端 IP         |
| `x.PathParams.Get(key)` | 获取路径参数          |

---

## 6. Handler 与中间件

### 6.1 泛型 Handler (推荐)

```go
// 框架自动解析参数、生成文档
func CreateUser(x *vigo.X, req *CreateReq) (*User, error) {
    // 业务逻辑
    return newUser, nil
}

// List 请求参数
type ListReq struct {
    Page  int    `json:"page" src:"query" default:"1"`
    Size  int    `json:"size" src:"query" default:"20"`
    Sort  string `json:"sort" src:"query"`
    Query string `json:"query" src:"query" desc:"模糊搜索"`
}

func ListUsers(x *vigo.X, req *ListReq) ([]*User, error) {
    // 业务逻辑
    return users, nil
}
```

### 6.2 中间件示例

```go
// Before: 鉴权
func AuthMiddleware(x *vigo.X) error {
    token := x.Request.Header.Get("Authorization")
    if token == "" {
        return vigo.NewError("Unauthorized").WithCode(401)
    }
    x.Set("user_id", "123")
    return nil
}

// After: 标准响应处理
import "github.com/veypi/vigo/contrib/common"

Router.After(common.JsonResponse, common.JsonErrorResponse)
```

---

## 7. 错误处理

采用 5 位数字编码，前三位对应 HTTP 状态码，后两位为场景细分：

### 7.1 预定义错误

```go
// 4xx 客户端错误
vigo.ErrBadRequest                    // 40000 - 通用请求错误
vigo.ErrInvalidArg.WithArgs("name")   // 40001 - 参数无效
vigo.ErrMissingArg.WithArgs("id")     // 40002 - 参数缺失
vigo.ErrArgFormat                     // 40003 - 参数格式错误

vigo.ErrUnauthorized                  // 40100 - 未登录/无token
vigo.ErrTokenInvalid                  // 40101 - token无效
vigo.ErrTokenExpired                  // 40102 - token过期
vigo.ErrNoPermission                  // 40103 - 无操作权限
vigo.ErrForbidden                     // 40300 - 禁止访问

vigo.ErrNotFound                      // 40400 - 资源不存在
vigo.ErrResourceNotFound.WithArgs("user") // 40401 - 指定资源不存在
vigo.ErrEndpointNotFound              // 40402 - 接口不存在

vigo.ErrConflict                      // 40900 - 资源冲突
vigo.ErrAlreadyExists.WithArgs("email")   // 40901 - 资源已存在

vigo.ErrTooManyRequests               // 42900 - 请求过于频繁

// 5xx 服务端错误
vigo.ErrInternalServer                // 50000 - 内部服务器错误
vigo.ErrDatabase                      // 50001 - 数据库错误
vigo.ErrCache                         // 50002 - 缓存错误
vigo.ErrThirdParty                    // 50003 - 第三方服务错误

vigo.ErrNotImplemented                // 50100 - 功能未实现
vigo.ErrNotSupported                  // 50101 - 不支持的操作

vigo.ErrServiceUnavailable            // 50300 - 服务不可用
```

### 7.2 错误使用方法

```go
// 基础使用
return nil, vigo.ErrNotFound

// 添加参数详情
return nil, vigo.ErrInvalidArg.WithArgs("email格式不正确")

// 包装底层错误
return nil, vigo.ErrDatabase.WithError(err)

// 自定义消息
return nil, vigo.ErrBadRequest.WithMessage("积分不足")

// 自定义错误码
return nil, vigo.NewError("业务错误").WithCode(40099)
```

## 8. 数据库 (GORM)

### 8.1 基础模型

```go
// 使用 vigo.Model，包含 UUID 主键和时间戳
type User struct {
    vigo.Model
    Name  string `json:"name"`
    Email string `json:"email"`
}
```

### 8.2 常用操作

```go
// 获取 DB 实例
cfg.DB()

// 查询
var user models.User
if err := cfg.DB().Where("id = ?", req.ID).First(&user).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, vigo.ErrNotFound
    }
    return nil, vigo.NewError("系统错误").WithError(err)
}
```

### 8.3 模型管理

```go
// models/init.go
var Models = &vigo.ModelList{}

func init() {
    Models.Add(&User{})
    Models.Add(&Order{})
}

// cli/main.go
models.Models.AutoMigrate(db)
```

---

## 9. 常用库

### 9.1 标准响应

```go
import "github.com/veypi/vigo/contrib/common"

Router.After(common.JsonResponse, common.JsonErrorResponse)
```

### 9.2 AES 加密

```go
import "github.com/veypi/vigo/contrib/config"

key := config.Key("your-secret-key")
encrypted, err := key.Encrypt("data")
decrypted, err := key.Decrypt(encrypted)
```

### 9.3 Redis

```go
import "github.com/veypi/vigo/contrib/config"

// 内存模式 (测试用)
redis := &config.Redis{Addr: "memory"}

// 真实 Redis
redis := &config.Redis{
    Addr:     "localhost:6379",
    Password: "password",
    DB:       0,
}
client := redis.Client()
```
