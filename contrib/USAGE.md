# Contrib 模块使用指南

## cache - 响应缓存

```go
import "github.com/veypi/vigo/contrib/cache"

// 创建缓存中间件 (默认 10s)
cm := cache.NewCacheMiddleware(handler, 30*time.Second)

// 使用
r.Get("/api/data", cm.Handler)

// 自定义清理间隔
cm.StartCleanup(5 * time.Minute)
```

## common - 通用工具

### 静态文件服务

```go
import "github.com/veypi/vigo/contrib/common"

// 本地目录 (需定义 {path:*} 参数)
r.Get("/static/{path:*}", common.Static("./static", "./404.html"))

// 单个文件
r.Get("/favicon.ico", common.Static("./favicon.ico", ""))

// Embed 目录
//go:embed static/*
var embedFS embed.FS
r.Get("/assets/{path:*}", common.EmbedDir(embedFS, "static", "./404.html"))

// Embed 单个文件
//go:embed logo.png
var logo []byte
r.Get("/logo.png", common.EmbedFile(logo, "image/png"))
```

### JSON 响应

```go
common.JsonResponse(x, data)
common.JsonErrorResponse(x, err)
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

## cors - 跨域处理

```go
import "github.com/veypi/vigo/contrib/cors"

// 允许任意跨域
r.Use(cors.AllowAny)

// 指定域名
corsMiddleware := cors.CorsAllow("https://app.com", "https://admin.com")
r.Use(corsMiddleware)
```

## crud - 自动 CRUD

```go
import "github.com/veypi/vigo/contrib/crud"

// 定义模型
type User struct {
    vigo.Model
    Name  string `json:"name"`
    Email string `json:"email"`
}

// 创建 CRUD 控制器
ctrl := crud.New(&User{})

// 注册全部路由 (需注入 db 到 context)
r.SetVar("db", func() *gorm.DB { return db })
ctrl.Register(r.SubRouter("/users"))

// 只注册指定动作
ctrl.Register(r.SubRouter("/users"), "list", "get")

// 自定义配置
ctrl.SetIDParam("user_id").
    SetListQueryFields("name", "email").
    Register(r)

// 生成的路由
// GET    /users        - List (分页/搜索)
// POST   /users        - Create
// GET    /users/{user_id}   - Get
// PATCH  /users/{user_id}   - Update (部分更新)
// DELETE /users/{user_id}   - Delete

// List 请求参数
type ListReq struct {
    Page  int    `src:"query" default:"1"`
    Size  int    `src:"query" default:"20"`
    Sort  string `src:"query"`
    Query string `src:"query"`  // 模糊搜索
}
```

## limiter - 限流

```go
import "github.com/veypi/vigo/contrib/limiter"

// 创建限流器 (窗口10秒, 最大100请求, 最小间隔100ms)
l := limiter.NewAdvancedRequestLimiter(
    10*time.Second,
    100,
    100*time.Millisecond,
)

// 启动清理协程
l.StartCleaner(5 * time.Minute)

// 作为中间件使用
r.Use(func(x *vigo.X) (any, error) {
    return l.Limit(x, nil)
})

// 自定义 key 生成
l = limiter.NewAdvancedRequestLimiter(
    10*time.Second, 100, 100*time.Millisecond,
    func(x *vigo.X) string {
        return x.GetRemoteIP()
    },
)

// 内置 key 函数
key := limiter.GetPathKeyFunc(x)  // ip:path
```

## proxy - 反向代理

```go
import "github.com/veypi/vigo/contrib/proxy"

// 反向代理到目标 (需定义 path 参数)
r.Any("/api/{path:*}", proxy.ProxyTo("http://backend:8080"))
```

## ufs - 联合文件系统

```go
import "github.com/veypi/vigo/contrib/ufs"

// 创建 UFS (按优先级搜索)
fs := ufs.New(
    "./local",           // 本地目录
    embedFS,             // embed.FS
    ufs.Embed(embedFS, "static"),  // 带前缀的 embed
)

// 使用
file, err := fs.Open("index.html")
entries, err := fs.ReadDir(".")
info, err := fs.Stat("file.txt")
```

## doc - 文档文件系统

```go
import "github.com/veypi/vigo/contrib/doc"

doc.New(router.SubRouter("/docs"), mdFiles, "docs")
```

## vhtml - UI 集成

```go
import "github.com/veypi/vigo/contrib/vhtml"

// 开发模式 (本地文件)
r.Get("/{path:*}", vhtml.New("./ui/dist"))

// 生产模式 (embed)
//go:embed dist/*
var uiFS embed.FS
r.Get("/{path:*}", vhtml.NewEmbed(uiFS, "dist"))
```

## plugin - 动态插件 (实验性)

```go
import "github.com/veypi/vigo/contrib/plugin"

// 创建加载器
loader := plugin.NewLoader("./plugins")

// 安全限制
loader.AddAllowedImports(
    "fmt",
    "github.com/veypi/vigo",
)
loader.AddForbiddenPrefixes(
    "os/exec",
    "syscall",
)

// 加载插件
p, err := loader.Load("myplugin.so")

// 调用函数
result, err := p.Call("Handler", x)
```

## sse - Server-Sent Events

```go
import "github.com/veypi/vigo/contrib/sse"

// SSE 已在核心 xwriter 中实现
// 使用 x.SSEWriter() 或 x.SSEEvent()

func handler(x *vigo.X) (any, error) {
    event := x.SSEEvent()

    go func() {
        for i := 0; i < 10; i++ {
            event("update", fmt.Sprintf(`{"count": %d}`, i))
            time.Sleep(time.Second)
        }
    }()

    return nil, nil
}
```
