---
name: vigo-backend
description: "Vigo Framework Backend Development Standards. Invoke this skill when developing RESTful APIs using Golang + Vigo Framework. It includes specifications for routing, parameter parsing, middleware, error handling, database operations, and more."
---

# Vigo Backend Development Standards

## Design Philosophy

Vigo follows an **Onion Architecture** with clear separation of concerns:

1. **Convention over Configuration** - Handlers return data, framework handles responses
2. **Type-Safe Parameter Binding** - Struct tags define sources, framework auto-parses
3. **Composable Middleware** - Before/Handler/After pipeline with parent-child inheritance
4. **Modular Apps** - Self-contained apps that can be integrated via Config + Router + Init

For detailed API documentation, use: `go doc github.com/veypi/vigo.X`

---

## 1. Request Pipeline

```
Before (parent → child) → Handler → After (child → parent)
```

| Stage   | Purpose                      | Key Behavior              |
| ------- | ---------------------------- | ------------------------- |
| Before  | Auth, validation, setup      | Parent runs before child  |
| Handler | Business logic               | Returns (Response, error) |
| After   | Response formatting, cleanup | Parent runs after child   |

**Handler Signature**:

```go
func Handler(x *vigo.X, req *Request) (*Response, error)
```

---

## 2. Project Structure

```
project/
├── api/                  # HTTP handlers
│   ├── init.go           # Root router + global middleware
│   └── {resource}/       # Per-resource module
│       ├── init.go       # Resource router
│       └── {action}.go   # Individual handlers
├── models/               # GORM models
├── cfg/                  # Configuration (DB, Redis, etc.)
├── libs/                 # Shared business logic
├── cli/main.go           # Entry point
└── init.go               # App definition
```

---

## 3. Routing

**Patterns**: `/{param}`, `/{param:[0-9]+}`, `/*`, `/**`

```go
// api/user/init.go
var Router = vigo.NewRouter()

func init() {
    Router.Use(PermCheck)
    Router.Get("/{id}", "Get User", getUser)
    Router.Post("/login", vigo.SkipBefore, auth, login)  // Skip parent Before
    Router.Post("/upload/{path:*}", handler, vigo.Stop)            // Stop after this

    msg := Router.SubRouter("msg")
    msg.Get("/", "List", listMessages)
}

// api/init.go
var Router = vigo.NewRouter().Use(Auth).After(common.JsonResponse, common.JsonErrorResponse)

func init() {
    Router.Extend("users", user.Router)
}
```

**Flow for /users/{id}**: Auth → PermCheck → getUser → JsonResponse

---

## 4. Parameter Binding

**Sources**: `path`, `query`, `header`, `json`, `form`

**Rules**:

- Non-pointer = required
- Pointer or `default` tag = optional

```go
type UpdateReq struct {
    ID       string  `src:"path@id"`              // Required path param
    Token    string  `src:"header@Authorization"` // Required header
    Verbose  bool    `src:"query" default:"false"` // Optional with default
    Name     string  `json:"name" src:"json"`      // Required body
    Email    *string `json:"email" src:"json"`     // Optional body
    Avatar   *multipart.FileHeader `src:"form"`     // File upload
}
```

---

## 5. Context Methods

See: `go doc github.com/veypi/vigo.X`

| Category | Methods                                    |
| -------- | ------------------------------------------ |
| Flow     | `Next()`, `Stop()`, `Skip(n)`              |
| Data     | `Set(k, v)`, `Get(k)`, `PathParams.Get(k)` |
| Response | `JSON(data)`, `WriteHeader(code)`          |

---

## 6. Handlers & Middleware

### Handler

```go
type ListReq struct {
    Page  int     `json:"page" src:"query" default:"1"`
    Size  int     `json:"size" src:"query" default:"20"`
    Sort  *string `json:"sort" src:"query"`
}
type ListResp struct {
    Total int64   `json:"total"`
    Items []*User `json:"items"`
}

func ListUsers(x *vigo.X, req *ListReq) (*ListResp, error) {
    return &ListResp{Total: 100, Items: users}, nil
}
```

### Middleware

```go
// Before: return error to abort
func Auth(x *vigo.X) error {
    if x.Request.Header.Get("Authorization") == "" {
        return vigo.ErrUnauthorized
    }
    x.Set("user_id", "123")
    return nil
}

// After: format response
Router.After(common.JsonResponse, common.JsonErrorResponse)
```

---

## 7. Error Handling

**Code Format**: `HTTP Status` + `Scenario` (e.g., `40001` = 400 + 01)

**Predefined Errors**: `ErrBadRequest`, `ErrInvalidArg`, `ErrMissingArg`, `ErrUnauthorized`, `ErrNoPermission`, `ErrNotFound`, `ErrConflict`, `ErrDatabase`, `ErrInternalServer`, etc.
See: go doc -all github.com/veypi/vigo | grep Err

```go
return nil, vigo.ErrNotFound
return nil, vigo.ErrInvalidArg.WithArgs("email")
return nil, vigo.ErrDatabase.WithError(err)
return nil, vigo.NewError("Balance Low").WithCode(40099)
```

---

## 8. Database (GORM)

See: `go doc github.com/veypi/vigo.Model`

```go
// models/user.go
type User struct {
    vigo.Model  // ID (UUID), CreatedAt, UpdatedAt, DeletedAt
    Name  string `json:"name"`
    Email string `json:"email"`
}

// models/init.go
var Models = &vigo.ModelList{}

func init() {
    Models.Add(&User{})
}

// Query pattern
var user models.User
if err := cfg.DB.Where("id = ?", req.ID).First(&user).Error; err != nil {
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
Router.After(common.JsonResponse, common.JsonErrorResponse)
```

### 9.2 Event Scheduler

```go
import "github.com/veypi/vigo/contrib/event"

event.Start()
event.Add("cleanup", func() error { ... }, event.Every(10*time.Second))
event.Add("init.db", func() error { ... })
event.Add("task.b", func() error { ... }, event.After("init.db"))
```

### 9.3 Auth Interface

```go
import "github.com/veypi/vigo/contrib/auth"

// cfg/config.go - TestAuth for dev, replace with real implementation in production
var Auth auth.Auth = &auth.TestAuth{}
// api/init.go

Router.Use(cfg.Auth.PermLogin)
Router.Post("resource", cfg.Auth.Perm("resource:create"), "description",createResource)
```

See: `go doc github.com/veypi/vigo/contrib/auth.Auth`

---

## 10. vigo App

App is a self-contained module with: Config + Router + Init function.

### 10.1 Basic Usage

```go
// cfg/config.go
package cfg

import "github.com/veypi/vigo/contrib/config"

type Options struct {
    SelfOption string         `json:"self_option"`
    DB         config.Database `json:"db"`
    Redis      config.Redis    `json:"redis"`
    Key        config.Key      `json:"key"`
}

var Global = &Options{}

var DB = Global.DB.Client
var Redis = Global.Redis.Client
```

```go
// init.go export: Router, Config, Init, otherFuncs...
package app

var Router = vigo.NewRouter()
var Config = cfg.Global

func init() {
  Router.Extend("api", api.Router)
  Router.Extend("ui", ui.Router)
  vhtml.WrapUI(Router, uifs)
}

func Init() error {
  // Initialization logic
  models.Models.Migrate(cfg.DB())
  return nil
}

```

```go
// cli/main.go
package main

import "myproject"

func main() {
  app := vigo.New("myapp", myproject.Router,myproject.Config, myproject.Init)
  panic(app.Run())
}
```

### 10.2 App Integration

To integrate another App, combine: **Config + Router + Init**

```go
// cfg/config.go - 1. Embed config
type Options struct {
    SelfOption string       `json:"self_option"`
    AuthCfg    auth.Config  `json:"auth"`
}
```

```go
// init.go - 2. Mount router, 3. Call init
func init() {
    Router.Extend("auth", auth.Router)
    Router.Extend("api", api.Router)
    api.Router.Use(auth.Middleware)
}

func Init() error {
    if err := auth.Init(); err != nil {
        return err
    }
    // Own initialization
    return nil
}
```
