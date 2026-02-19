//
// auth.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package auth

import (
	"context"
	"errors"

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

// ErrUnauthorized 未授权错误
var ErrUnauthorized = errors.New("unauthorized")

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
}
