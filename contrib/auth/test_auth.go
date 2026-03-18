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

// TestProvider 是默认测试鉴权实现。
// 所有鉴权通过，查询返回模拟数据，便于模块在未注入真实实现时开发联调。
type TestProvider struct{}

// TestAuth 兼容旧命名。
type TestAuth = TestProvider

var _ Provider = (*TestProvider)(nil)

func (a *TestProvider) UserID(x *vigo.X) string {
	return "test"
}

func (a *TestProvider) Grant(ctx context.Context, userID, permCode string, permLevel int) error {
	return nil
}

func (a *TestProvider) Revoke(ctx context.Context, userID, permCode string) error {
	return nil
}

func (a *TestProvider) Check(ctx context.Context, userID, permCode string, permLevel int) bool {
	return true
}

func (a *TestProvider) ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error) {
	return map[string]int{
		"test_resource_1": LevelRead,
		"test_resource_2": LevelAdmin,
	}, nil
}

func (a *TestProvider) ListUsers(ctx context.Context, permCode string) (map[string]int, error) {
	return map[string]int{
		"test_user_001": LevelAdmin,
		"test_user_002": LevelRead,
	}, nil
}

func (a *TestProvider) GrantRole(ctx context.Context, userID, roleCode string) error {
	return nil
}

func (a *TestProvider) RevokeRole(ctx context.Context, userID, roleCode string) error {
	return nil
}

func (a *TestProvider) AddRole(roleCode, roleName string, permPolicies ...string) error {
	return nil
}
