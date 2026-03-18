# Auth 权限系统设计

---

## 一、术语与权限码层级

### 1.0 术语

| 名称 | 含义 | 是否允许变量 | 示例 |
|------|------|--------------|------|
| `perm_code` | 完整静态权限码，用于存储、授予、撤销、实现端检查 | 否 | `org:orgA` |
| `perm_level` | 权限等级，必须严格按单双层规则解释 | 否 | `1` `2` `4` `6` `7` |
| `perm_expr` | 调用端中间件使用的动态检查模板，请求期解析为 `perm_code` | 是 | `org:{orgID@query}` |
| `perm_policy` | 角色策略字符串，格式为 `perm_code:perm_level` | 否 | `org:*:7` |

### 1.1 层级定义

```
层级从 1 开始计数：
- 奇数层（第1、3、5层）：资源类型
- 偶数层（第2、4、6层）：实例
```

### 1.2 示例

```
org                      → 第1层 (奇数) - 资源类型
org:orgA                → 第2层 (偶数) - 实例
org:orgA:project        → 第3层 (奇数) - 资源类型
org:orgA:project:projB → 第4层 (偶数) - 实例
```

### 1.3 perm_code 结构规则

- **单层 (奇数层)**: 资源类型层，如 `org`、`org:orgA:project`
- **双层 (偶数层)**: 资源实例层，如 `org:orgA`、`org:orgA:project:projB`
- **通配符 `*`**: 只能出现在最后一层，代表对该层所有实例的权限

**通配符示例**:

| perm_code | 含义 |
|-----------|------|
| `org:*:2` | 对所有组织有读权限 |
| `org:org_A:*:1` | 对 org_A 下所有资源类型有创建权限 |
| `org:org_A:project:*:7` | 对 org_A 下所有项目有管理员权限 |
| `*:7` | 对所有资源有管理员权限（特殊全局权限）|

**约束**:
- `*` 必须是完整的 segment，不能是 `org*` 或 `*org`
- `*` 只能出现在最后一层
- `*:7` 是特殊权限，必须是 level 7

---

## 二、perm_level 规则

### 2.1 奇数层（资源类型）

| level | 二进制 | 含义 |
| ----- | ------ | -------------------- |
| 0     | 000    | 无权限 |
| 1     | 001    | 可创建该类型的子资源 |

### 2.2 偶数层（实例）

| level | 二进制 | 含义 |
| ----- | ------ | ---------------------- |
| 0     | 000    | 无权限 |
| 2     | 010    | 读取 |
| 4     | 100    | 写入（修改，不能删除） |
| 6     | 110    | 读写（读取+修改） |
| 7     | 111    | 管理员（完全控制：读写+删除+授权） |

---

## 三、检查规则

### 3.1 层级与权限对应

| 权限 | perm_level | 检查层级 | 说明 |
|------|-------|----------|------|
| 创建 | 1 | 奇数层 | 检查资源类型层 |
| 读取 | 2 | 偶数层 | 检查实例层 |
| 写入 | 4 | 偶数层 | 检查实例层 |
| 读写 | 6 | 偶数层 | 检查实例层 |
| 管理 | 7 | 偶数层 | 检查实例层 |

### 3.2 具体规则

```
创建资源 (perm_level 1)
  → 检查当前 perm_code 对应的奇数层
  → 例: "org:orgA:project" 检查 "org:orgA:project" 层

读取/更新/删除资源 (perm_level 2,4,6,7)
  → 检查当前 perm_code 对应的偶数层
  → 如无权限，递归向上检查父实例层
  → 注意：只有 Level 7 (管理员) 权限才会向下继承，Level 2,4,6 不会继承
  → 例: "org:orgA:project:projB" 先检查实例层，再检查 "org:orgA"
```

### 3.3 perm_policy 规则

`perm_policy` 是角色上的完整策略串，格式必须是：

```text
perm_code:perm_level
```

示例：

```text
*:7
org:*:7
org:org_id:*:7
```

约束：

- `perm_policy` 不能包含变量，占位符如 `{orgID}` 是非法的。
- `perm_level` 必须使用上面定义的合法值，并按单双层规则解释。
- `perm_code` 是否带 `*` 由实现端解释，但仍然必须遵守资源层级规则。

---

## 四、权限流程示例

### 场景一：用户 A 创建组织

```
1. 用户A创建组织 "公司A"
2. 自动创建权限:
   - perm_code: "org:org_companyA"
   - perm_level: 7 (创建者完全控制)
```

### 场景二：用户 A 邀请用户 B 加入组织

```
1. 用户A授予用户B: org:org_companyA perm_level 2 (读)
2. 用户B权限表:
   - org:org_companyA level 2
3. 用户B可执行:
   - ✓ 读取 org_companyA
   - ✗ 修改/删除
```

### 场景三：用户 B 创建项目

```
前置: 用户B有 org:org_companyA level 2 (读)，需要额外授权

1. 用户A授予用户B: org:org_companyA:project perm_level 1 (创建项目)
2. 用户B创建项目 "项目X"
3. 自动创建权限:
   - perm_code: "org:org_companyA:project:project_X"
   - perm_level: 7
```

### 场景四：用户 C 加入项目并创建文档

```
1. 用户B授予用户C: org:org_companyA:project:project_X perm_level 2 (读)
2. 用户C需要额外授权才能创建文档
3. 用户C创建文档 "文档Y"
4. 自动创建权限:
   - perm_code: "org:org_companyA:project:project_X:doc:doc_Y"
   - perm_level: 7
```

---

## 五、Auth 对象与 Provider 设计

```go
package auth

import (
	"context"
	"github.com/veypi/vigo"
)

// ========== 权限等级 ==========

const (
	LevelNone      = 0
	LevelCreate    = 1 // 001 创建 (检查奇数层)
	LevelRead      = 2 // 010 读取 (检查偶数层)
	LevelWrite     = 4 // 100 写入 (检查偶数层)
	LevelReadWrite = 6 // 110 读写 (检查偶数层)
	LevelAdmin     = 7 // 111 管理员 (完全控制)
)

// PermFunc 权限检查函数类型
type PermFunc func(x *vigo.X) error

// Provider 是实现端需要实现的 SPI
type Provider interface {
	UserID(x *vigo.X) string
	Grant(ctx context.Context, userID, permCode string, permLevel int) error
	Revoke(ctx context.Context, userID, permCode string) error
	Check(ctx context.Context, userID, permCode string, permLevel int) bool
	ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error)
	ListUsers(ctx context.Context, permCode string) (map[string]int, error)
	GrantRole(ctx context.Context, userID, roleCode string) error
	RevokeRole(ctx context.Context, userID, roleCode string) error
	AddRole(roleCode, roleName string, permPolicies ...string) error
}

// Auth 是业务模块持有的统一鉴权对象
type Auth struct {
	// 注入的实现端
}

func New(provider ...Provider) *Auth
func (a *Auth) SetProvider(provider Provider) *Auth

// ========== 上下文 ==========
func (a *Auth) UserID(x *vigo.X) string

// ========== 登录检查 ==========
func (a *Auth) Login() PermFunc

// ========== 权限检查 ==========
func (a *Auth) Require(permExpr string, permLevel int) PermFunc
func (a *Auth) RequireCreate(permExpr string) PermFunc
func (a *Auth) RequireRead(permExpr string) PermFunc
func (a *Auth) RequireWrite(permExpr string) PermFunc
func (a *Auth) RequireAdmin(permExpr string) PermFunc

// ========== 权限授予（业务调用） ==========
func (a *Auth) Grant(ctx context.Context, userID, permCode string, permLevel int) error
func (a *Auth) Revoke(ctx context.Context, userID, permCode string) error

// ========== 权限查询 ==========
func (a *Auth) Check(ctx context.Context, userID, permCode string, permLevel int) bool

// ========== 资源列表查询 ==========
func (a *Auth) ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error)
func (a *Auth) ListUsers(ctx context.Context, permCode string) (map[string]int, error)
func (a *Auth) GrantRole(ctx context.Context, userID, roleCode string) error
func (a *Auth) RevokeRole(ctx context.Context, userID, roleCode string) error
func (a *Auth) AddRole(roleCode, roleName string, permPolicies ...string) error
```

### 5.1 职责边界

- `Provider` 是实现端接口，由接入方实现真实鉴权逻辑。
- `Auth` 是调用端对象，由业务模块长期持有，并把 `Login()`、`RequireXXX()` 等方法暴露给路由和业务逻辑。
- 模块代码只依赖 `*auth.Auth` 或 `auth.Auth`，不要直接依赖实现端接口。
- 调用方必须在处理请求前显式执行 `SetProvider(...)`，未注入时运行期会 panic。
- `Provider` 只接收静态 `perm_code` 和 `perm_level`，不处理动态变量解析。
- `Auth.Require(...)` 只接收 `perm_expr`，先在请求期解析成 `perm_code`，再交给 `Provider.Check(...)`。

### 5.2 初始化方式

```go
// 模块内部
package cfg

import "github.com/veypi/vigo/contrib/auth"

var Auth auth.Auth

// 接入方初始化
func Init() error {
	cfg.Auth.SetProvider(myAuthProvider)

	if err := cfg.Auth.AddRole("user", "Default User", "org:1", "xxx:1"); err != nil {
		return err
	}
	if err := cfg.Auth.AddRole("admin", "System Admin", "*:7"); err != nil {
		return err
	}
	return nil
}
```

约定：

- 模块初始化阶段应显式初始化默认角色 `user` 和 `admin`。
- `user` 一般承载系统默认创建类权限，例如 `org:1`、`xxx:1`。
- `admin` 一般承载系统全局管理权限，推荐直接使用 `*:7`。

### 5.3 测试实现

```go
testAuth := auth.New(&auth.TestProvider{})
```

`TestProvider` 仅用于测试或本地联调，不会自动注入；未设置 Provider 会直接 panic。

## 5.4 数据结构

```go
// Permission 用户权限
type Permission struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	PermissionID  string `json:"permission_id"`
	Level         int    `json:"level"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}
```

---

## 六、使用示例

### 6.1 固定写法

```go
var Router = vigo.NewRouter()

func init() {
	// 创建组织 - 需要系统级 org 权限
	Router.Post("/orgs", cfg.Auth.RequireCreate("org"), CreateOrg)

	// 超级管理员接口
	Router.Get("/admin/users", cfg.Auth.RequireAdmin("*"), AdminListUsers)
}
```

### 6.2 perm_expr 动态解析

```go
func init() {
	// 默认从 context 获取
	Router.Get("/orgs/current", cfg.Auth.RequireRead("org:{orgID}"), GetOrg)

	// 从 query 参数获取
	// GET /orgs?orgID=xxx
	Router.Get("/orgs", cfg.Auth.RequireRead("org:{orgID@query}"), GetOrg)

	// 从 path 参数获取
	// GET /orgs/{orgID}
	Router.Get("/orgs/{orgID}", cfg.Auth.RequireRead("org:{orgID@path}"), GetOrg)

	// 多层嵌套
	// GET /orgs/{orgID}/projects/{projectID}
	Router.Get("/orgs/{orgID}/projects/{projectID}",
		cfg.Auth.RequireRead("org:{orgID@path}:project:{projectID@path}"),
		GetProject,
	)
}
```

### 6.3 完整示例

```go
var Router = vigo.NewRouter().Use(cfg.Auth.Login())

func init() {
	// 创建组织 - 系统级权限
	Router.Post("/orgs", cfg.Auth.RequireCreate("org"), CreateOrg)

	// 列出我的组织 - 只需登录
	Router.Get("/orgs", ListMyOrgs)

	// 组织操作 - 从路径获取
	Router.Get("/orgs/{orgID}", cfg.Auth.RequireRead("org:{orgID@path}"), GetOrg)
	Router.Put("/orgs/{orgID}", cfg.Auth.RequireWrite("org:{orgID@path}"), UpdateOrg)
	Router.Delete("/orgs/{orgID}", cfg.Auth.RequireAdmin("org:{orgID@path}"), DeleteOrg)

	// 项目操作 - 嵌套资源
	Router.Post("/orgs/{orgID}/projects", cfg.Auth.RequireCreate("org:{orgID@path}:project"), CreateProject)
	Router.Get("/orgs/{orgID}/projects/{projectID}", cfg.Auth.RequireRead("org:{orgID@path}:project:{projectID@path}"), GetProject)
	Router.Put("/orgs/{orgID}/projects/{projectID}", cfg.Auth.RequireWrite("org:{orgID@path}:project:{projectID@path}"), UpdateProject)
	Router.Delete("/orgs/{orgID}/projects/{projectID}", cfg.Auth.RequireAdmin("org:{orgID@path}:project:{projectID@path}"), DeleteProject)

	// 文档操作
	Router.Post("/orgs/{orgID}/projects/{projectID}/docs", cfg.Auth.RequireCreate("org:{orgID@path}:project:{projectID@path}:doc"), CreateDoc)
	Router.Get("/orgs/{orgID}/projects/{projectID}/docs/{docID}", cfg.Auth.RequireRead("org:{orgID@path}:project:{projectID@path}:doc:{docID@path}"), GetDoc)
}
```

### 6.4 接入方实现示例

```go
type MyProvider struct{}

func (p *MyProvider) UserID(x *vigo.X) string { return "user-1" }
func (p *MyProvider) Check(ctx context.Context, userID, permCode string, permLevel int) bool {
	return true
}
func (p *MyProvider) Grant(ctx context.Context, userID, permCode string, permLevel int) error {
	return nil
}
func (p *MyProvider) Revoke(ctx context.Context, userID, permCode string) error {
	return nil
}
func (p *MyProvider) ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error) {
	return nil, nil
}
func (p *MyProvider) ListUsers(ctx context.Context, permCode string) (map[string]int, error) {
	return nil, nil
}
func (p *MyProvider) GrantRole(ctx context.Context, userID, roleCode string) error {
	return nil
}
func (p *MyProvider) RevokeRole(ctx context.Context, userID, roleCode string) error {
	return nil
}
func (p *MyProvider) AddRole(roleCode, roleName string, permPolicies ...string) error {
	return nil
}

func Init() error {
	cfg.Auth.SetProvider(&MyProvider{})
	return nil
}
```

### 6.5 perm_expr 解析规则

| 语法 | 来源 | 示例 |
|------|------|------|
| `{key}` | context 变量 | `{orgID}` |
| `{key@ctx}` | context 变量 | `{orgID@ctx}` |
| `{key@path}` | path 参数 | `{orgID@path}` |
| `{key@query}` | query 参数 | `{orgID@query}` |
| `{key@header}` | header | `{orgID@header}` |

说明：

- `perm_expr` 只用于调用端中间件，不进入实现端存储。
- `perm_expr` 解析后的结果必须是完整静态 `perm_code`。
- `perm_expr` 不携带 `perm_level`，等级由 `Require(..., permLevel)` 或 `RequireRead/RequireWrite/...` 指定。

---

## 七、接口说明

### 7.1 业务调用 vs 管理端

此接口是**业务层**调用，用于：
- 创建资源时自动授予权限
- 业务逻辑中检查权限

**管理端**（如权限管理后台）可以通过直接操作数据库实现批量管理。

### 7.2 Grant 调用时机

```go
// 创建组织时
func CreateOrg(x *vigo.X, req *CreateOrgReq) (*OrgResp, error) {
	org := models.Org{Name: req.Name}
	db.Create(&org)

	// 授予创建者完全控制权限
	cfg.Auth.Grant(x.Context(), userID, "org:"+org.ID, auth.LevelAdmin)

	return &OrgResp{Org: org}, nil
}
```

### 7.3 列表与搜索接口设计

对于资源列表（List）或搜索接口，推荐以下设计模式：

1. **全量管理接口**（如后台管理系统）：
   - 使用 `RequireAdmin("*")` 或 `RequireAdmin("org:*")`。
   - 这类接口返回所有数据，必须严格控制权限。

2. **用户侧列表/搜索**（如“我的项目”）：
   - **方式一（仅所有者）**：
     - 使用 `Login()` 确保登录。
     - 业务层：`db.Where("owner_id = ?", userID).Find(&orgs)`
   - **方式二（协作模式 - 使用 ListResources）**：
     - 调用 Auth 接口获取有权限的 ID 列表。
     - `perms, _ := auth.ListResources(ctx, userID, "org")`
     - `ids := keys(perms)`
     - 业务层：`db.Where("id IN ?", ids).Find(&orgs)`
   - **方式三（混合模式）**：
     - 同时查询 owner_id 和 授权列表。
     - `perms, _ := auth.ListResources(...)`
     - `ids := keys(perms)`
     - `db.Where("owner_id = ? OR id IN ?", userID, ids).Find(&orgs)`

3. **`RequireXXX` 适用场景**：
   - 针对 **特定资源实例** 的操作（URL 中包含 ID，如 `/projects/{id}`）。
   - 针对 **共享资源** 的访问控制。
   - 针对 **管理功能** 的鉴权。

### 7.4 角色策略示例

```go
if err := cfg.Auth.AddRole("user", "Default User", "org:1", "xxx:1"); err != nil {
	return err
}

if err := cfg.Auth.AddRole("admin", "System Admin", "*:7"); err != nil {
	return err
}

if err := cfg.Auth.AddRole(
	"org_admin",
	"Organization Admin",
	"org:*:7",
); err != nil {
	return err
}
```

规则：

- `permPolicies` 中的每一项都必须是完整 `perm_policy`，格式为 `perm_code:perm_level`。
- 不允许写动态变量，如 `org:{orgID}:7` 是非法策略。
- `*` 只能是完整 segment，且只能出现在最后一段。
- `org:*` 是合法尾部通配；`org:*:project:*` 是非法策略。
- `*:7` 是特殊全局权限，必须是 level 7。
