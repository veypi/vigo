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

// TestAuth 测试用 Auth 实现
// 所有鉴权通过，查询返回空/false，创建返回 nil
type TestAuth struct{}

// 确保实现 Auth 接口
var _ Auth = (*TestAuth)(nil)

// ========== 上下文 ==========

// UserID 获取当前用户ID
func (a *TestAuth) UserID(x *vigo.X) string {
	return "test"
}

// Login 检查用户是否登录
func (a *TestAuth) Login() PermFunc {
	return func(x *vigo.X) error {
		return nil // 总是通过
	}
}

// ========== 权限检查 ==========

// Perm 检查权限
func (a *TestAuth) Perm(code string, level int) PermFunc {
	return func(x *vigo.X) error {
		return nil // 总是通过
	}
}

// PermCreate 检查创建权限 (level 1)
func (a *TestAuth) PermCreate(code string) PermFunc {
	return a.Perm(code, LevelCreate)
}

// PermRead 检查读取权限 (level 2)
func (a *TestAuth) PermRead(code string) PermFunc {
	return a.Perm(code, LevelRead)
}

// PermWrite 检查更新权限 (level 4)
func (a *TestAuth) PermWrite(code string) PermFunc {
	return a.Perm(code, LevelWrite)
}

// PermAdmin 检查管理员权限 (level 7)
func (a *TestAuth) PermAdmin(code string) PermFunc {
	return a.Perm(code, LevelAdmin)
}

// ========== 权限授予（业务调用） ==========

// Grant 授予权限
func (a *TestAuth) Grant(ctx context.Context, userID, permissionID string, level int) error {
	return nil // 创建成功
}

// Revoke 撤销权限
func (a *TestAuth) Revoke(ctx context.Context, userID, permissionID string) error {
	return nil // 撤销成功
}

// ========== 权限查询 ==========

// Check 检查权限
func (a *TestAuth) Check(ctx context.Context, userID, permissionID string, level int) bool {
	return true // 总是通过
}

// ListResources 查询用户在特定资源类型下的详细权限信息
func (a *TestAuth) ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error) {
	// 返回一个模拟数据，方便测试列表过滤逻辑
	// 假设用户对 "test_resource_1" 有读权限，对 "test_resource_2" 有管理员权限
	return map[string]int{
		"test_resource_1": LevelRead,
		"test_resource_2": LevelAdmin,
	}, nil
}

// ListUsers 查询特定资源的所有协作者及其权限
func (a *TestAuth) ListUsers(ctx context.Context, permissionID string) (map[string]int, error) {
	// 返回一个模拟数据
	return map[string]int{
		"test_user_001": LevelAdmin,
		"test_user_002": LevelRead,
	}, nil
}

// ========== 角色管理 ==========

// GrantRole 授予角色
func (a *TestAuth) GrantRole(ctx context.Context, userID, roleCode string) error {
	return nil // 授予成功
}

// RevokeRole 撤销角色
func (a *TestAuth) RevokeRole(ctx context.Context, userID, roleCode string) error {
	return nil // 撤销成功
}

// AddRole 添加角色定义 (用于初始化)
func (a *TestAuth) AddRole(code, name string, policies ...string) error {
	return nil // 添加成功
}
