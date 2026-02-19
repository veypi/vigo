# Auth 权限系统设计

---

## 一、权限码层级

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

---

## 二、权限等级

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

| 权限 | level | 检查层级 | 说明 |
|------|-------|----------|------|
| 创建 | 1 | 奇数层 | 检查资源类型层 |
| 读取 | 2 | 偶数层 | 检查实例层 |
| 写入 | 4 | 偶数层 | 检查实例层 |
| 读写 | 6 | 偶数层 | 检查实例层 |
| 管理 | 7 | 偶数层 | 检查实例层 |

### 3.2 具体规则

```
创建资源 (level 1)
  → 检查当前 permissionID 对应的奇数层
  → 例: "org:{orgID}:project" 检查 "org:{orgID}:project" 层

读取/更新/删除资源 (level 2,4,6,7)
  → 检查当前 permissionID 对应的偶数层
  → 如无权限，递归向上检查父实例层
  → 注意：只有 Level 7 (管理员) 权限才会向下继承，Level 2,4,6 不会继承
  → 例: "org:{orgID}:project:{projectID}" 先检查实例层，再检查 "org:{orgID}"
```

---

## 四、权限流程示例

### 场景一：用户 A 创建组织

```
1. 用户A创建组织 "公司A"
2. 自动创建权限:
   - PermissionID: "org:org_companyA"
   - Level: 7 (创建者完全控制)
```

### 场景二：用户 A 邀请用户 B 加入组织

```
1. 用户A授予用户B: org:org_companyA level 2 (读)
2. 用户B权限表:
   - org:org_companyA level 2
3. 用户B可执行:
   - ✓ 读取 org_companyA
   - ✗ 修改/删除
```

### 场景三：用户 B 创建项目

```
前置: 用户B有 org:org_companyA level 2 (读)，需要额外授权

1. 用户A授予用户B: org:org_companyA:project level 1 (创建项目)
2. 用户B创建项目 "项目X"
3. 自动创建权限:
   - PermissionID: "org:org_companyA:project:project_X"
   - Level: 7
```

### 场景四：用户 C 加入项目并创建文档

```
1. 用户B授予用户C: org:org_companyA:project:project_X level 2 (读)
2. 用户C需要额外授权才能创建文档
3. 用户C创建文档 "文档Y"
4. 自动创建权限:
   - PermissionID: "org:org_companyA:project:project_X:doc:doc_Y"
   - Level: 7
```

---

## 五、Auth 接口设计

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

// Auth 权限管理接口
type Auth interface {
	// ========== 上下文 ==========

	// UserID 获取当前用户ID
	UserID(x *vigo.X) string

	// ========== 登录检查 ==========

	// Login 检查用户是否登录
	Login() PermFunc

	// ========== 权限检查 ==========

	// Perm 检查权限
	// code: 权限码，支持动态解析
	//   - 固定写法: "org:orgA"
	//   - 动态解析: "org:{orgID}" 从 path 获取
	//               "org:{orgID@query}" 从 query 获取
	//               "org:{orgID@header}" 从 header 获取
	//               "org:{orgID@ctx}" 从 ctx 获取
	// level: 需要的权限等级
	Perm(code string, level int) PermFunc

	// ========== 快捷方法 ==========

	// PermCreate 检查创建权限 (level 1，检查奇数层)
	PermCreate(code string) PermFunc

	// PermRead 检查读取权限 (level 2，检查偶数层)
	PermRead(code string) PermFunc

	// PermWrite 检查更新权限 (level 4，检查偶数层)
	PermWrite(code string) PermFunc

	// PermAdmin 检查管理员权限 (level 7，检查偶数层)
	PermAdmin(code string) PermFunc

	// ========== 权限授予（业务调用） ==========

	// Grant 授予权限
	// 在创建资源、被授权等业务逻辑中调用
	// permissionID: 权限码，如 "org:orgA"
	// level: 权限等级
	Grant(ctx context.Context, userID, permissionID string, level int) error

	// Revoke 撤销权限
	Revoke(ctx context.Context, userID, permissionID string) error

	// ========== 权限查询 ==========

	// Check 检查权限 不支持动态解析
	// permissionID: 完整的权限码，如 "org:orgA"
	Check(ctx context.Context, userID, permissionID string, level int) bool

	// ========== 资源列表查询 ==========

	// ListResources 查询用户在特定资源类型下的详细权限信息
	// 用于解决 "查询我有权限的 org 列表" 等场景
	// userID: 用户ID
	// resourceType: 资源类型 (奇数层)，如 "org" 或 "org:{orgID}:project"
	// 返回: map[实例ID]权限等级 (如 {"orgA": 2, "orgB": 7})
	ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error)

	// ListUsers 查询特定资源的所有协作者及其权限
	// 用于解决 "查看这个项目有哪些成员" 等场景
	// permissionID: 资源实例权限码，如 "org:orgA"
	// 返回: map[用户ID]权限等级 (如 {"user1": 2, "user2": 7})
	ListUsers(ctx context.Context, permissionID string) (map[string]int, error)
}

// ========== 数据结构 ==========

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
	Router.Post("/orgs", cfg.Auth.PermCreate("org"), CreateOrg)

	// 超级管理员接口
	Router.Get("/admin/users", cfg.Auth.PermAdmin("*"), AdminListUsers)
}
```

### 6.2 动态解析

```go
func init() {
	// 从路径参数获取 orgID (默认)
	// GET /orgs/{orgID}
	Router.Get("/orgs/{orgID}", cfg.Auth.PermRead("org:{orgID}"), GetOrg)

	// 从 query 参数获取
	// GET /orgs?orgID=xxx
	Router.Get("/orgs", cfg.Auth.PermRead("org:{orgID@query}"), GetOrg)

	// 多层嵌套
	// GET /orgs/{orgID}/projects/{projectID}
	Router.Get("/orgs/{orgID}/projects/{projectID}",
		cfg.Auth.PermRead("org:{orgID}:project:{projectID}"),
		GetProject,
	)
}
```

### 6.3 完整示例

```go
var Router = vigo.NewRouter().Use(cfg.Auth.Login())

func init() {
	// 创建组织 - 系统级权限
	Router.Post("/orgs", cfg.Auth.PermCreate("org"), CreateOrg)

	// 列出我的组织 - 只需登录
	Router.Get("/orgs", ListMyOrgs)

	// 组织操作 - 从路径获取
	Router.Get("/orgs/{orgID}", cfg.Auth.PermRead("org:{orgID}"), GetOrg)
	Router.Put("/orgs/{orgID}", cfg.Auth.PermWrite("org:{orgID}"), UpdateOrg)
	Router.Delete("/orgs/{orgID}", cfg.Auth.PermAdmin("org:{orgID}"), DeleteOrg)

	// 项目操作 - 嵌套资源
	Router.Post("/orgs/{orgID}/projects", cfg.Auth.PermCreate("org:{orgID}:project"), CreateProject)
	Router.Get("/orgs/{orgID}/projects/{projectID}", cfg.Auth.PermRead("org:{orgID}:project:{projectID}"), GetProject)
	Router.Put("/orgs/{orgID}/projects/{projectID}", cfg.Auth.PermWrite("org:{orgID}:project:{projectID}"), UpdateProject)
	Router.Delete("/orgs/{orgID}/projects/{projectID}", cfg.Auth.PermAdmin("org:{orgID}:project:{projectID}"), DeleteProject)

	// 文档操作
	Router.Post("/orgs/{orgID}/projects/{projectID}/docs", cfg.Auth.PermCreate("org:{orgID}:project:{projectID}:doc"), CreateDoc)
	Router.Get("/orgs/{orgID}/projects/{projectID}/docs/{docID}", cfg.Auth.PermRead("org:{orgID}:project:{projectID}:doc:{docID}"), GetDoc)
}
```

### 6.4 动态解析规则

| 语法 | 来源 | 示例 |
|------|------|------|
| `{key}` | path 参数 | `{orgID}` |
| `{key@query}` | query 参数 | `{orgID@query}` |
| `{key@header}` | header | `{orgID@header}` |
| `{key@ctx}` | context | `{orgID@ctx}` |

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
   - 使用 `PermAdmin("*")` 或 `PermAdmin("org:*")`。
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

3. **`PermXXX` 适用场景**：
   - 针对 **特定资源实例** 的操作（URL 中包含 ID，如 `/projects/{id}`）。
   - 针对 **共享资源** 的访问控制。
   - 针对 **管理功能** 的鉴权。
