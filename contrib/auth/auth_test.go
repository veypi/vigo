package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/veypi/vigo"
)

type stubProvider struct {
	userID        string
	checkResult   bool
	lastPermCode  string
	lastPermLevel int
}

func (s *stubProvider) UserID(x *vigo.X) string {
	return s.userID
}

func (s *stubProvider) Check(ctx context.Context, userID, permCode string, permLevel int) bool {
	s.lastPermCode = permCode
	s.lastPermLevel = permLevel
	return s.checkResult
}

func (s *stubProvider) Grant(ctx context.Context, userID, permCode string, permLevel int) error {
	return nil
}

func (s *stubProvider) Revoke(ctx context.Context, userID, permCode string) error {
	return nil
}

func (s *stubProvider) ListResources(ctx context.Context, userID, resourceType string) (map[string]int, error) {
	return nil, nil
}

func (s *stubProvider) ListUsers(ctx context.Context, permCode string) (map[string]int, error) {
	return nil, nil
}

func (s *stubProvider) GrantRole(ctx context.Context, userID, roleCode string) error {
	return nil
}

func (s *stubProvider) RevokeRole(ctx context.Context, userID, roleCode string) error {
	return nil
}

func (s *stubProvider) AddRole(roleCode, roleName string, permPolicies ...string) error {
	return nil
}

func newTestX() *vigo.X {
	req, _ := http.NewRequest(http.MethodGet, "/orgs/org-1?project_id=proj-2", nil)
	req.Header.Set("X-Org", "org-header")
	x := &vigo.X{
		Request: req,
		PathParams: vigo.PathParams{
			{Key: "orgID", Value: "org-1"},
		},
	}
	x.Set("orgID", "org-ctx")
	x.Set("tenantID", "tenant-9")
	return x
}

func TestAuthZeroValuePanicsWithoutProvider(t *testing.T) {
	var a Auth
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when provider is not set")
		}
	}()
	if got := a.UserID(newTestX()); got != "" {
		t.Fatalf("expected empty user id before panic, got %q", got)
	}
}

func TestAuthPermResolvesDynamicPermission(t *testing.T) {
	provider := &stubProvider{userID: "u-1", checkResult: true}
	a := New(provider)

	err := a.Require("org:{orgID}:project:{project_id@query}:tenant:{tenantID}:header:{X-Org@header}:path:{orgID@path}", LevelRead)(newTestX())
	if err != nil {
		t.Fatalf("expected perm check to pass, got %v", err)
	}

	want := "org:org-ctx:project:proj-2:tenant:tenant-9:header:org-header:path:org-1"
	if provider.lastPermCode != want {
		t.Fatalf("expected perm code %q, got %q", want, provider.lastPermCode)
	}
	if provider.lastPermLevel != LevelRead {
		t.Fatalf("expected level %d, got %d", LevelRead, provider.lastPermLevel)
	}
}

func TestAuthPermReturnsUnauthorized(t *testing.T) {
	a := New(&stubProvider{userID: "", checkResult: true})
	err := a.Login()(newTestX())
	if !errors.Is(err, vigo.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestAuthPermReturnsNoPermission(t *testing.T) {
	a := New(&stubProvider{userID: "u-1", checkResult: false})
	err := a.RequireRead("org:{orgID}")(newTestX())
	if !errors.Is(err, vigo.ErrNoPermission) {
		t.Fatalf("expected no permission, got %v", err)
	}
}

func TestAuthUsesExplicitTestProvider(t *testing.T) {
	a := New(&TestProvider{})
	if got := a.UserID(newTestX()); got != "test" {
		t.Fatalf("expected injected test provider user id test, got %q", got)
	}
}

func TestRequirePanicsOnInvalidPermLevel(t *testing.T) {
	a := New(&TestProvider{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid perm level")
		}
	}()
	_ = a.Require("org:{orgID@path}", 3)
}

func TestRequirePanicsOnEmptyPermExpr(t *testing.T) {
	a := New(&TestProvider{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for empty perm expr")
		}
	}()
	_ = a.Require("", LevelRead)
}

func TestRequirePanicsOnInvalidPermExprSource(t *testing.T) {
	a := New(&TestProvider{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid perm expr source")
		}
	}()
	_ = a.Require("org:{orgID@cookie}", LevelRead)
}

func TestRequirePanicsOnSegmentParityMismatch(t *testing.T) {
	a := New(&TestProvider{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for perm expr and perm level parity mismatch")
		}
	}()
	_ = a.RequireRead("org")
}

func TestRequireAllowsGlobalWildcardOnlyForAdmin(t *testing.T) {
	a := New(&TestProvider{})
	if middleware := a.RequireAdmin("*"); middleware == nil {
		t.Fatal("expected middleware for admin wildcard")
	}
}

func TestRequireAllowsWildcardWithAnyLevel(t *testing.T) {
	a := New(&TestProvider{})
	// Wildcard can be used with any perm_level, not just LevelAdmin
	// org:* has 2 segments (even), suitable for Read/Write (even levels)
	if middleware := a.Require("org:*", LevelRead); middleware == nil {
		t.Fatal("expected middleware for wildcard with read level")
	}
	// org:orgA:*:1 has 3 segments (odd), suitable for Create (odd level)
	if middleware := a.Require("org:orgA:*", LevelCreate); middleware == nil {
		t.Fatal("expected middleware for wildcard with create level")
	}
}

func TestRequirePanicsOnWildcardNotAtEnd(t *testing.T) {
	a := New(&TestProvider{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for non-terminal wildcard")
		}
	}()
	_ = a.RequireAdmin("org:*:project:*")
}

func TestRequirePanicsOnPartialWildcardSegment(t *testing.T) {
	a := New(&TestProvider{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for partial wildcard segment")
		}
	}()
	_ = a.RequireAdmin("org:foo*")
}

func TestAddRoleAllowsWildcardWithAnyLevel(t *testing.T) {
	a := New(&TestProvider{})
	// Wildcard can be used with any perm_level in role policies
	if err := a.AddRole("org_reader", "Org Reader", "org:*:2"); err != nil {
		t.Fatalf("expected AddRole to succeed with wildcard and read level: %v", err)
	}
	if err := a.AddRole("org_creator", "Org Creator", "org:orgA:*:1"); err != nil {
		t.Fatalf("expected AddRole to succeed with wildcard and create level: %v", err)
	}
}
