//
// auth.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package auth

import (
	"context"

	"github.com/veypi/vigo"
)

// Auth 权限管理接口 (Vigo 框架专用)
type Auth interface {
	// ========== 上下文信息提取 ==========
	UserID(x *vigo.X) string
	OrgID(x *vigo.X) string

	// ========== 中间件生成 ==========
	// PermLogin 检查用户是否登录
	PermLogin(*vigo.X) error

	// Perm 检查权限
	// permissionID 格式: "resource:action", 例如 "user:read", "order:*", "*:*"
	Perm(permissionID string) func(*vigo.X) error

	// PermOnResource 检查特定资源权限
	// resourceKey 从 x.PathParams 获取,如果不存在则从 Query 获取
	// permissionID 格式: "resource:action"
	PermOnResource(permissionID, resourceKey string) func(*vigo.X) error

	// PermAny 满足任一权限即可
	// permissionIDs 格式: "resource:action"
	PermAny(permissionIDs ...string) func(*vigo.X) error

	// PermAll 必须满足所有权限
	// permissionIDs 格式: "resource:action"
	PermAll(permissionIDs ...string) func(*vigo.X) error

	// ========== 角色管理 ==========
	// AddRole 添加角色定义
	// policies 格式: "resource:action", 例如 "user:read", "*:*"
	AddRole(roleCode, roleName string, policies ...string) error

	// GetRole 获取角色定义
	GetRole(roleCode string) (*Role, error)

	// ListRoles 列出所有角色
	ListRoles() ([]*Role, error)

	// ListUserRoles 查询用户的角色列表
	ListUserRoles(ctx context.Context, userID, orgID string) ([]string, error)

	// ========== 权限管理 ==========
	// GrantRole 授予角色
	GrantRole(ctx context.Context, userID, orgID, roleCode string) error

	// GrantRoles 批量授予角色
	GrantRoles(ctx context.Context, userID, orgID string, roleCodes ...string) error

	// RevokeRole 撤销角色
	RevokeRole(ctx context.Context, userID, orgID, roleCode string) error

	// RevokeRoles 批量撤销角色
	RevokeRoles(ctx context.Context, userID, orgID string, roleCodes ...string) error

	// GrantResourcePerm 授予特定资源权限
	// permissionID 格式: "resource:action"
	GrantResourcePerm(ctx context.Context, userID, orgID, permissionID, resourceID string) error

	// RevokeResourcePerm 撤销特定资源权限
	// permissionID 格式: "resource:action"
	RevokeResourcePerm(ctx context.Context, userID, orgID, permissionID, resourceID string) error

	// RevokeAll 撤销用户所有权限
	RevokeAll(ctx context.Context, userID, orgID string) error

	// ========== 权限查询 ==========
	// CheckPerm 检查权限
	// permissionID 格式: "resource:action"
	// resourceID: "*" 表示所有资源,或指定具体资源ID
	CheckPerm(ctx context.Context, userID, orgID, permissionID, resourceID string) bool

	// ListUserPermissions 列出用户所有权限
	ListUserPermissions(ctx context.Context, userID, orgID string) ([]*UserPermission, error)

	// ListResourceUsers 列出资源的授权用户
	// permissionID 格式: "resource:action"
	ListResourceUsers(ctx context.Context, orgID, permissionID, resourceID string) ([]*ResourceUser, error)
}

// Role 角色定义
type Role struct {
	Code        string   `json:"code" desc:"角色代码"`
	Name        string   `json:"name" desc:"角色名称"`
	Policies    []string `json:"policies" desc:"权限策略: resource:action"`
	Description string   `json:"description" desc:"描述"`
}

// UserPermission 用户权限
type UserPermission struct {
	Resource   string   `json:"resource" desc:"资源类型: user, order, *"`
	ResourceID string   `json:"resource_id" desc:"资源ID, * 表示所有"`
	Actions    []string `json:"actions" desc:"允许的操作: read, write, delete"`
}

// ResourceUser 资源授权用户
type ResourceUser struct {
	UserID  string   `json:"user_id" desc:"用户ID"`
	Actions []string `json:"actions" desc:"允许的操作"`
}
