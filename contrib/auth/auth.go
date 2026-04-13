//
// auth.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/veypi/vigo"
)

const (
	// LevelNone means no permission.
	LevelNone = 0
	// LevelCreate grants create permission on type segments.
	LevelCreate = 1
	// LevelRead grants read permission on instance segments.
	LevelRead = 2
	// LevelWrite grants write permission on instance segments.
	LevelWrite = 4
	// LevelReadWrite grants read and write permission on instance segments.
	LevelReadWrite = 6
	// LevelAdmin grants full control on instance segments.
	LevelAdmin = 7
)

// PermFunc is a Vigo middleware function used for auth checks.
type PermFunc func(x *vigo.X) error

// Provider 是实现端需要实现的 SPI。
// 业务模块持有 Auth 对象，由调用方注入具体 Provider。
type Provider interface {
	// UserID 从请求中提取并验证用户身份凭证（如 token），返回用户 ID。
	// 如果凭证不存在或无效，应返回空字符串。
	// 该方法由 Login 中间件调用，用于判断用户是否已登录。
	UserID(x *vigo.X) string
	Check(ctx context.Context, userID, permCode string, permLevel int) bool
	Grant(ctx context.Context, userID, permCode string, permLevel int) error
	Revoke(ctx context.Context, userID, permCode string) error
	ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error)
	ListUsers(ctx context.Context, permCode string) (map[string]int, error)
	GrantRole(ctx context.Context, userID, roleCode string) error
	RevokeRole(ctx context.Context, userID, roleCode string) error
	AddRole(roleCode, roleName string, permPolicies ...string) error
}

// Auth is the calling-side authorization object used by business modules.
//
// Integrators must implement Provider and inject it with SetProvider before any
// request is served. Using Auth before provider injection panics.
//
// Core terms:
//
//   - perm_code: a fully resolved static permission code used by Provider,
//     such as "org:orgA", "org:orgA:project:projB", or "org:*"
//   - perm_level: the required permission level; valid values are 1, 2, 4, 6,
//     and 7, interpreted with odd/even segment rules
//   - perm_expr: a request-time permission expression used by Require...
//     methods; it is resolved into perm_code before Provider.Check runs
//   - perm_policy: a complete role policy string in the form
//     "perm_code:perm_level"; it must not contain variables
//
// Segment rules:
//
//   - odd segments are resource types
//   - even segments are resource instances
//
// Examples:
//
//   - "org" is a type segment
//   - "org:orgA" is an instance segment
//   - "org:orgA:project" is a type segment
//   - "org:orgA:project:projB" is an instance segment
//
// Permission levels:
//
//   - 1: create, checked on type segments
//   - 2: read, checked on instance segments
//   - 4: write, checked on instance segments
//   - 6: read+write, checked on instance segments
//   - 7: admin, checked on instance segments
//
// Usage guidance:
//
// Most user-owned resources only need Login middleware. The handler can then
// enforce ownership by comparing business data such as user_id. Require...
// middleware is intended for shared resources and admin-style management APIs,
// where access is determined by explicit resource permissions rather than
// simple ownership.
//
// Require expressions:
//
// Require and its helpers accept perm_expr values. Placeholders default to
// context values. Supported sources are:
//
//   - "{orgID}" or "{orgID@ctx}" from x.Get("orgID")
//   - "{orgID@path}" from path params
//   - "{orgID@query}" from query params
//   - "{Authorization@header}" from headers
//
// Wildcard rules are strict:
//
//   - "*" is a special-case shorthand for global admin and is only valid with
//     perm_level 7
//   - a wildcard segment must be exactly "*" and may appear only in the last
//     segment
//   - "org:*" is valid for admin-style checks, but "org:*:project:*" is not
//
// Initialization convention:
//
// Applications usually initialize two built-in roles during startup:
//
//   - "user" for default create-style permissions such as "org:1"
//   - "admin" for full-system access, usually with the single policy "*:7"
//
// Example:
//
//	var Auth auth.Auth
//
//	func Init() error {
//		Auth.SetProvider(&MyProvider{})
//		if err := Auth.AddRole("user", "Default User", "org:1", "xxx:1"); err != nil {
//			return err
//		}
//		if err := Auth.AddRole("admin", "System Admin", "*:7"); err != nil {
//			return err
//		}
//		return nil
//	}
//
//	var Router = vigo.NewRouter().Use(Auth.Login())
//
//	func init() {
//		Router.Get("/me/orgs", listMyOrgs)
//		Router.Get("/orgs/{orgID}", Auth.RequireRead("org:{orgID@path}"), getSharedOrg)
//		Router.Get("/admin/users", Auth.RequireAdmin("*"), listUsers)
//	}
type Auth struct {
	provider Provider
}

// New creates an Auth object and optionally injects its Provider.
func New(provider ...Provider) *Auth {
	a := &Auth{}
	if len(provider) > 0 {
		a.SetProvider(provider[0])
	}
	return a
}

// SetProvider injects the Provider implementation used by this Auth object.
func (a *Auth) SetProvider(provider Provider) *Auth {
	a.provider = provider
	return a
}

// Provider returns the injected Provider and panics if none has been set.
func (a *Auth) Provider() Provider {
	return a.currentProvider()
}

func (a *Auth) currentProvider() Provider {
	if a == nil || a.provider == nil {
		panic("auth provider is not set")
	}
	return a.provider
}

// UserID delegates to Provider.UserID.
func (a *Auth) UserID(x *vigo.X) string {
	return a.currentProvider().UserID(x)
}

// Login returns middleware that rejects requests without a current user.
func (a *Auth) Login() PermFunc {
	return func(x *vigo.X) error {
		if a.UserID(x) == "" {
			return vigo.ErrUnauthorized
		}
		return nil
	}
}

// Require 使用 permExpr 构造请求期鉴权中间件。
// permExpr 允许动态占位，默认从 context 取值；显式来源支持 @path/@query/@header/@ctx。
func (a *Auth) Require(permExpr string, permLevel int) PermFunc {
	mustValidateRequire(permExpr, permLevel)
	return func(x *vigo.X) error {
		userID := a.UserID(x)
		if userID == "" {
			return vigo.ErrUnauthorized
		}
		permCode, err := resolvePermCode(x, permExpr)
		if err != nil {
			return err
		}
		if !a.currentProvider().Check(x.Context(), userID, permCode, permLevel) {
			return vigo.ErrNoPermission
		}
		return nil
	}
}

// RequireCreate is shorthand for Require(permExpr, LevelCreate).
func (a *Auth) RequireCreate(permExpr string) PermFunc {
	return a.Require(permExpr, LevelCreate)
}

// RequireRead is shorthand for Require(permExpr, LevelRead).
func (a *Auth) RequireRead(permExpr string) PermFunc {
	return a.Require(permExpr, LevelRead)
}

// RequireWrite is shorthand for Require(permExpr, LevelWrite).
func (a *Auth) RequireWrite(permExpr string) PermFunc {
	return a.Require(permExpr, LevelWrite)
}

// RequireAdmin is shorthand for Require(permExpr, LevelAdmin).
func (a *Auth) RequireAdmin(permExpr string) PermFunc {
	return a.Require(permExpr, LevelAdmin)
}

// Grant assigns a static perm_code and perm_level to a user.
func (a *Auth) Grant(ctx context.Context, userID, permCode string, permLevel int) error {
	return a.currentProvider().Grant(ctx, userID, permCode, permLevel)
}

// Revoke removes a static perm_code from a user.
func (a *Auth) Revoke(ctx context.Context, userID, permCode string) error {
	return a.currentProvider().Revoke(ctx, userID, permCode)
}

// Check delegates static permission checking to the Provider.
func (a *Auth) Check(ctx context.Context, userID, permCode string, permLevel int) bool {
	return a.currentProvider().Check(ctx, userID, permCode, permLevel)
}

// ListResources lists instance permissions under a resource type.
func (a *Auth) ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error) {
	return a.currentProvider().ListResources(ctx, userID, resourceType)
}

// ListUsers lists collaborators and their levels for a static perm_code.
func (a *Auth) ListUsers(ctx context.Context, permCode string) (map[string]int, error) {
	return a.currentProvider().ListUsers(ctx, permCode)
}

// GrantRole assigns a role to a user.
func (a *Auth) GrantRole(ctx context.Context, userID, roleCode string) error {
	return a.currentProvider().GrantRole(ctx, userID, roleCode)
}

// RevokeRole removes a role from a user.
func (a *Auth) RevokeRole(ctx context.Context, userID, roleCode string) error {
	return a.currentProvider().RevokeRole(ctx, userID, roleCode)
}

// AddRole registers a role definition with complete perm_policy strings.
func (a *Auth) AddRole(roleCode, roleName string, permPolicies ...string) error {
	for _, permPolicy := range permPolicies {
		mustValidatePermPolicy(permPolicy)
	}
	return a.currentProvider().AddRole(roleCode, roleName, permPolicies...)
}

func mustValidateRequire(permExpr string, permLevel int) {
	mustValidatePermLevel(permLevel)
	mustValidatePermExpr(permExpr, permLevel)
}

func mustValidatePermLevel(permLevel int) {
	switch permLevel {
	case LevelCreate, LevelRead, LevelWrite, LevelReadWrite, LevelAdmin:
		return
	default:
		panic(fmt.Sprintf("invalid perm_level %d", permLevel))
	}
}

func mustValidatePermExpr(permExpr string, permLevel int) {
	if strings.TrimSpace(permExpr) == "" {
		panic("perm_expr is empty")
	}
	segments := strings.Split(permExpr, ":")
	for idx, segment := range segments {
		if segment == "" {
			panic(fmt.Sprintf("invalid perm_expr %q: empty segment", permExpr))
		}
		mustValidatePermSegment(segment, permExpr)
		mustValidateWildcardSegment(segment, idx, len(segments), permLevel, permExpr)
	}
	if len(segments) == 1 && segments[0] == "*" {
		if permLevel != LevelAdmin {
			panic(fmt.Sprintf("invalid perm_expr %q: wildcard * is only valid with admin level (7), got level %d", permExpr, permLevel))
		}
		return
	}
	segCount := len(segments)
	if permLevel == LevelCreate && segCount%2 == 0 {
		panic(fmt.Sprintf("invalid perm_expr %q for create perm_level: expected odd segment count", permExpr))
	}
	if permLevel != LevelCreate && segCount%2 != 0 {
		panic(fmt.Sprintf("invalid perm_expr %q for perm_level %d: expected even segment count", permExpr, permLevel))
	}
}

func mustValidatePermPolicy(permPolicy string) {
	if strings.TrimSpace(permPolicy) == "" {
		panic("perm_policy is empty")
	}
	idx := strings.LastIndexByte(permPolicy, ':')
	if idx <= 0 || idx == len(permPolicy)-1 {
		panic(fmt.Sprintf("invalid perm_policy %q", permPolicy))
	}
	permCode := permPolicy[:idx]
	permLevelText := permPolicy[idx+1:]
	permLevel, err := strconv.Atoi(permLevelText)
	if err != nil {
		panic(fmt.Sprintf("invalid perm_policy %q: invalid perm_level", permPolicy))
	}
	mustValidatePermLevel(permLevel)
	mustValidateStaticPermCode(permCode, permLevel, permPolicy)
}

func mustValidateStaticPermCode(permCode string, permLevel int, source string) {
	if strings.TrimSpace(permCode) == "" {
		panic(fmt.Sprintf("invalid %s: perm_code is empty", source))
	}
	if strings.ContainsAny(permCode, "{}") {
		panic(fmt.Sprintf("invalid %s: perm_code must not contain variables", source))
	}
	segments := strings.Split(permCode, ":")
	for idx, segment := range segments {
		if segment == "" {
			panic(fmt.Sprintf("invalid %s: empty segment", source))
		}
		mustValidateWildcardSegment(segment, idx, len(segments), permLevel, source)
	}
	if len(segments) == 1 && segments[0] == "*" {
		if permLevel != LevelAdmin {
			panic(fmt.Sprintf("invalid %s: wildcard * is only valid with admin level (7), got level %d", source, permLevel))
		}
		return
	}
	segCount := len(segments)
	if permLevel == LevelCreate && segCount%2 == 0 {
		panic(fmt.Sprintf("invalid %s: create perm_level requires odd segment count", source))
	}
	if permLevel != LevelCreate && segCount%2 != 0 {
		panic(fmt.Sprintf("invalid %s: perm_level %d requires even segment count", source, permLevel))
	}
}

func mustValidatePermSegment(segment, permExpr string) {
	start := strings.IndexByte(segment, '{')
	if start < 0 {
		if strings.ContainsRune(segment, '}') {
			panic(fmt.Sprintf("invalid perm_expr %q: unexpected '}'", permExpr))
		}
		return
	}
	end := strings.IndexByte(segment, '}')
	if end < 0 || end < start {
		panic(fmt.Sprintf("invalid perm_expr %q: unclosed placeholder", permExpr))
	}
	if strings.IndexByte(segment[end+1:], '{') >= 0 || strings.ContainsRune(segment[end+1:], '}') {
		panic(fmt.Sprintf("invalid perm_expr %q: multiple placeholders in one segment", permExpr))
	}
	if strings.ContainsRune(segment[:start], '}') {
		panic(fmt.Sprintf("invalid perm_expr %q: unexpected '}'", permExpr))
	}
	mustValidatePlaceholder(segment[start+1:end], permExpr)
}

func mustValidatePlaceholder(placeholder, permExpr string) {
	parts := strings.SplitN(placeholder, "@", 2)
	key := strings.TrimSpace(parts[0])
	if key == "" {
		panic(fmt.Sprintf("invalid perm_expr %q: placeholder key is empty", permExpr))
	}
	if !utf8.ValidString(key) {
		panic(fmt.Sprintf("invalid perm_expr %q: placeholder key is invalid", permExpr))
	}
	source := "ctx"
	if len(parts) == 2 {
		source = strings.TrimSpace(parts[1])
	}
	switch source {
	case "ctx", "path", "query", "header":
		return
	default:
		panic(fmt.Sprintf("invalid perm_expr %q: unsupported source %q", permExpr, source))
	}
}

func mustValidateWildcardSegment(segment string, index, total, permLevel int, source string) {
	if !strings.Contains(segment, "*") {
		return
	}
	if segment != "*" {
		panic(fmt.Sprintf("invalid %s: wildcard must be a whole segment", source))
	}
	if index != total-1 {
		panic(fmt.Sprintf("invalid %s: wildcard must appear only in the last segment", source))
	}
}

func resolvePermCode(x *vigo.X, permExpr string) (string, error) {
	var builder strings.Builder
	for {
		start := strings.IndexByte(permExpr, '{')
		if start < 0 {
			builder.WriteString(permExpr)
			return builder.String(), nil
		}
		end := strings.IndexByte(permExpr[start:], '}')
		if end < 0 {
			return "", vigo.ErrInvalidArg.WithArgs("perm_expr")
		}
		end += start
		builder.WriteString(permExpr[:start])
		placeholder := permExpr[start+1 : end]
		value, err := resolvePlaceholder(x, placeholder)
		if err != nil {
			return "", err
		}
		builder.WriteString(value)
		permExpr = permExpr[end+1:]
	}
}

func resolvePlaceholder(x *vigo.X, placeholder string) (string, error) {
	parts := strings.SplitN(placeholder, "@", 2)
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", vigo.ErrMissingArg.WithArgs("permission placeholder")
	}

	source := "ctx"
	if len(parts) == 2 {
		source = strings.TrimSpace(parts[1])
	}

	switch source {
	case "path":
		value := x.PathParams.Get(key)
		if value == "" {
			return "", vigo.ErrMissingArg.WithArgs(key)
		}
		return value, nil
	case "query":
		value := x.Request.URL.Query().Get(key)
		if value == "" {
			return "", vigo.ErrMissingArg.WithArgs(key)
		}
		return value, nil
	case "header":
		value := x.Request.Header.Get(key)
		if value == "" {
			return "", vigo.ErrMissingArg.WithArgs(key)
		}
		return value, nil
	case "ctx":
		value := x.Get(key)
		if value == nil {
			return "", vigo.ErrMissingArg.WithArgs(key)
		}
		if str, ok := value.(string); ok {
			if str == "" {
				return "", vigo.ErrMissingArg.WithArgs(key)
			}
			return str, nil
		}
		return fmt.Sprint(value), nil
	default:
		return "", vigo.ErrInvalidArg.WithArgs("permission source")
	}
}
