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
	return "test_user_001"
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
