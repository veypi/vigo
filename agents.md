---
name: vigo-backend
description: "Vigo Framework Backend Development Standards. Invoke this skill when developing RESTful APIs using Golang + Vigo Framework. It includes specifications for routing, parameter parsing, middleware, error handling, database operations, and more."
---

# Vigo Backend Development Standards

## 1. Core Principles: Onion Architecture

Request processing pipeline: `Before` → `Handler` → `After`

| Stage   | Responsibility                                   | Description                                                   |
| ------- | ------------------------------------------------ | ------------------------------------------------------------- |
| Before  | Authentication, preprocessing, context injection | Executed upon entry. **Parent hooks run before child hooks.** |
| Handler | Business logic                                   | Returns data object, **does not write response directly.**    |
| After   | Response formatting, logging, cleanup            | Executed upon exit. **Parent hooks run after child hooks.**   |

**Key Rule**: Use generic Handlers `func(*vigo.X, *Req) (Resp, error)`. The framework automatically binds parameters and generates documentation.

---

## 2. Project Structure

```text
/
├── api/
│   ├── init.go          # API root router (registers global middleware)
│   └── {resource}/      # Resource module (e.g., user, order)
│       ├── init.go      # Resource router & sub-router definitions & register router
│       ├── {action}.go  # Handlers (get.go, create.go, list.go)
├── models/              # GORM Data Models
│   ├── init.go          # Model registration (Models.Add)
│   └── {resource}.go    # Struct definitions
├── cfg/                 # Configuration & Infrastructure (DB, Redis)
├── libs/                # Common Business Logic Libraries
├── cli/main.go          # Application Entry Point
└── init.go              # Project Root Router Integration
```

---

## 3. Routing System

**Syntax**: `/{param}` (Named), `/{param:[0-9]+}` (Regex), `/*` (Wildcard), `/**` (Recursive)

**Registration Example**:

```go
// api/user/init.go
var Router = vigo.NewRouter()

func init() {
    Router.Use(PermCheck)                           // Middleware
    Router.Get("/{id}", "Get User", getUser)        // Standard
    Router.Post("/login", vigo.SkipBefore, login)   // Skip Middleware
    Router.Post("/fs",customwrite, vigo.Stop)       // Stop Middleware

    msgRouter := Router.SubRouter("msg")            // Sub-router
    msgRouter.Get("/", "List Messages", listMessages)
}

// api/init.go (Mounting)
func init() {
    Router.Use(middleware.AuthRequired)
    Router.After(common.JsonResponse)
    Router.Extend("/users", user.Router)
}
```

---

## 4. Parameter Parsing

**Source**: `path`, `query`, `header`, `json`, `form`.
**Rule**: Non-pointer fields are **required** by default. Use pointer or `default` tag for optional.

**Example**:

```go
type UserUpdateReq struct {
    UserID  string  `src:"path@user_id"`         // Path (Required)
    TraceID string  `src:"header@X-Trace-ID"`    // Header (Required)
    Verbose bool    `src:"query" default:"false"`// Query (Optional + Default)
    Name    string  `json:"name" src:"json"`     // Body (Required)
    Email   *string `json:"email" src:"json"`    // Body (Optional)
    Avatar  *multipart.FileHeader `src:"form"`   // File
}
```

---

## 5. Context (`*vigo.X`) Methods

- **Flow**: `Next()`, `Stop()`, `Skip(n)`
- **Data**: `Set(k, v)`, `Get(k)`, `PathParams.Get(k)`
- **Resp**: `JSON(data)`, `WriteHeader(code)`

---

## 6. Handlers & Middleware

### 6.1 Generic Handler

```go
type ListReq struct {
    Page int    `json:"page" src:"query" default:"1"`
    Size int    `json:"size" src:"query" default:"20"`
    Sort *string `json:"sort" src:"query" desc:"Sort field:'name desc'"`
    Query *string `json:"query" src:"query" desc:"Search query"`
}
type ListResp struct {
    Total int64 `json:"total"`
    Items []*User `json:"items"`
}

func ListUsers(x *vigo.X, req *ListReq) (*ListResp, error) {
    // Logic...
    return users, nil
}
```

### 6.2 Middleware

```go
// Before: Auth
func AuthMiddleware(x *vigo.X) error {
    if x.Request.Header.Get("Authorization") == "" {
        return vigo.ErrUnauthorized // 40100
    }
    x.Set("user_id", "123")
    return nil
}

// After: Response
Router.After(common.JsonResponse, common.JsonErrorResponse)
```

## 7. Error Handling

**Format**: `3-digit Status` + `2-digit Scenario` (e.g., `400` + `01` = `40001`).
**Common Errors**: `vigo.ErrBadRequest`, `vigo.ErrInvalidArg`, `vigo.ErrMissingArg`, `vigo.ErrArgFormat`, `vigo.ErrUnauthorized`, `vigo.ErrTokenInvalid`, `vigo.ErrTokenExpired`, `vigo.ErrNoPermission`, `vigo.ErrForbidden`, `vigo.ErrNotFound`, `vigo.ErrResourceNotFound`, `vigo.ErrEndpointNotFound`, `vigo.ErrConflict`, `vigo.ErrAlreadyExists`, `vigo.ErrTooManyRequests`, `vigo.ErrInternalServer`, `vigo.ErrDatabase`, `vigo.ErrCache`, `vigo.ErrThirdParty`, `vigo.ErrNotImplemented`, `vigo.ErrNotSupported`, `vigo.ErrServiceUnavailable`.

**Usage Patterns**:

```go
return nil, vigo.ErrNotFound                                  // Simple
return nil, vigo.ErrInvalidArg.WithArgs("email")              // With Args
return nil, vigo.ErrDatabase.WithError(err)                   // Wrap Error
return nil, vigo.NewError("Balance Low").WithCode(40099)      // Custom
```

## 8. Database (GORM)

### 8.1 Model Definition

```go
type User struct {
    vigo.Model // Includes: ID (UUID), CreatedAt, UpdatedAt, DeletedAt
    Name  string `json:"name"`
    Email string `json:"email"`
}
```

### 8.2 Registration & Migration

```go
// models/init.go
var AllModels = &vigo.ModelList{}
func init() {
    Models.Add(&User{})
}
// cli/main.go
models.Models.AutoMigrate(db)
```

### 8.3 Common Operations

```go
// Query by ID
var user models.User
if err := cfg.DB().Where("id = ?", req.ID).First(&user).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, vigo.ErrNotFound
    }
    return nil, vigo.ErrDatabase.WithError(err)
}
```

---

## 9. Contrib Libraries

### 9.1 Standard Response

```go
import "github.com/veypi/vigo/contrib/common"
// Registers standard JSON success/error formatters
Router.After(common.JsonResponse, common.JsonErrorResponse)
```

### 9.2 vigo Event (Task Scheduler)

**Features**: Local/Distributed, Periodic/Scheduled, One-time/Daemon.

```go
import "github.com/veypi/vigo/contrib/event"

// 1. Setup Event
// cli/main.go
event.Start()
// 2. Periodic Task
event.Add("xxx.periodic", func() error , event.Every(10*time.Second))
// 3. One-time & Dependency (Before/After invalid for Every/At)
event.Add("xxx.init.db", func() error {  Models.AutoMigrate(db)/db.Save/.... })
event.Add("xxx.task_B", funcB, event.After("xxx.init.db")) // B runs after init.db
event.Add("xxx.task_C", funcC, event.Before("xxx.init.db"))// C runs before init.db
```

### 9.3 vigo app

```go
// define cfg options
// cfg/config.go
import "github.com/veypi/vigo/contrib/config"
type Options struct {
    selfOption string
    DB config.Database  // .Type .DSN
    Redis  config.Redis    `json:"redis"` // .Addr .Password .DB
    Key config.Key    `json:"key"` // string key.Encrypt("data"), key.Decrypt(encrypted)
    OtherAppCfg OtherAppConfig `json:"other_app_cfg"` // other app config
}
var Config *Options = &Options{}
var DB = Config.DB.Client // cfg.DB() => GORM DB Client
var Redis = Config.Redis.Client // cfg.Redis() => redis client

// start app
// cli/main.go
import "github.com/veypi/vigo/flags"
var cliOpts = &struct {
    Host string `json:"host" default:"0.0.0.0"`
    Port int    `json:"port" short:"p" default:"8080"`
    *cfg.Options
}{
    Options: cfg.Config, // Bind Global Config
}
// Auto-adds: -h (help), -f (config file), -l (log level)
var cmd = flags.New("app_name", "description", cliOpts)
func main() {
    cmd.Command = runWeb
    _ = cmd.Run()
}
func runWeb() error {
    // Start Vigo
    server, _ := vigo.New(vigo.WithHost(cliOpts.Host), vigo.WithPort(cliOpts.Port))
    server.SetRouter(app.Router) // init.go.Router
    return server.Run()
}
// define root router
// init.go
var Router = vigo.NewRouter()
var _ = Router.Extend("api", api.Router) // my app api router: /api/**
var _ = Router.Extend("otherApp", otherApp.Router) // other app router: /otherApp/**
var _ = vhtml.WrapUI(Router, uifs) // static files: /**
```
