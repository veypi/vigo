//
// test_auth.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package auth

import (
	"context"

	"github.com/veypi/vigo"
)

// TestAuth 测试用 Auth 实现 (所有鉴权通过,查询返回空,创建返回 nil)
type TestAuth struct{}

// 确保实现 Auth 接口
var _ Auth = (*TestAuth)(nil)

// ========== 上下文信息提取 ==========

func (a *TestAuth) UserID(x *vigo.X) string {
	return "test_user_001"
}

func (a *TestAuth) OrgID(x *vigo.X) string {
	return "test_org_001"
}

// ========== 中间件生成 ==========

func (a *TestAuth) PermLogin(*vigo.X) error {
	return nil // 总是通过
}

func (a *TestAuth) Perm(permissionID string) func(*vigo.X) error {
	return func(x *vigo.X) error {
		return nil // 总是通过
	}
}

func (a *TestAuth) PermOnResource(permissionID, resourceKey string) func(*vigo.X) error {
	return func(x *vigo.X) error {
		return nil // 总是通过
	}
}

func (a *TestAuth) PermAny(permissionIDs ...string) func(*vigo.X) error {
	return func(x *vigo.X) error {
		return nil // 总是通过
	}
}

func (a *TestAuth) PermAll(permissionIDs ...string) func(*vigo.X) error {
	return func(x *vigo.X) error {
		return nil // 总是通过
	}
}

// ========== 角色管理 ==========

func (a *TestAuth) AddRole(roleCode, roleName string, policies ...string) error {
	return nil // 创建成功
}

func (a *TestAuth) GetRole(roleCode string) (*Role, error) {
	return nil, nil // 查询为空
}

func (a *TestAuth) ListRoles() ([]*Role, error) {
	return []*Role{}, nil // 返回空列表
}

func (a *TestAuth) ListUserRoles(ctx context.Context, userID, orgID string) ([]string, error) {
	return []string{}, nil // 返回空列表
}

// ========== 权限管理 ==========

func (a *TestAuth) GrantRole(ctx context.Context, userID, orgID, roleCode string) error {
	return nil // 创建成功
}

func (a *TestAuth) GrantRoles(ctx context.Context, userID, orgID string, roleCodes ...string) error {
	return nil // 创建成功
}

func (a *TestAuth) RevokeRole(ctx context.Context, userID, orgID, roleCode string) error {
	return nil // 撤销成功
}

func (a *TestAuth) RevokeRoles(ctx context.Context, userID, orgID string, roleCodes ...string) error {
	return nil // 撤销成功
}

func (a *TestAuth) GrantResourcePerm(ctx context.Context, userID, orgID, permissionID, resourceID string) error {
	return nil // 创建成功
}

func (a *TestAuth) RevokeResourcePerm(ctx context.Context, userID, orgID, permissionID, resourceID string) error {
	return nil // 撤销成功
}

func (a *TestAuth) RevokeAll(ctx context.Context, userID, orgID string) error {
	return nil // 撤销成功
}

// ========== 权限查询 ==========

func (a *TestAuth) CheckPerm(ctx context.Context, userID, orgID, permissionID, resourceID string) bool {
	return true // 总是通过
}

func (a *TestAuth) ListUserPermissions(ctx context.Context, userID, orgID string) ([]*UserPermission, error) {
	return []*UserPermission{}, nil // 返回空列表
}

func (a *TestAuth) ListResourceUsers(ctx context.Context, orgID, permissionID, resourceID string) ([]*ResourceUser, error) {
	return []*ResourceUser{}, nil // 返回空列表
}
