package authscope

import (
	"context"
	"strings"

	"starter-kit/utils"
)

type contextKey string

const scopeContextKey contextKey = "auth_scope"

type Scope struct {
	UserID           string
	Username         string
	Role             string
	Permissions      map[string]struct{}
	IsImpersonated   bool
	OriginalUserID   string
	OriginalUsername string
	OriginalRole     string
}

func New(userID, username, role string, permissions []string) Scope {
	permissionSet := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		key := normalizePermissionKey(permission)
		if key == "" {
			continue
		}
		permissionSet[key] = struct{}{}
	}

	return Scope{
		UserID:      strings.TrimSpace(userID),
		Username:    strings.TrimSpace(username),
		Role:        strings.TrimSpace(role),
		Permissions: permissionSet,
	}
}

func NewFromClaims(claims map[string]any, permissions []string) Scope {
	scope := New(
		utils.InterfaceString(claims["user_id"]),
		utils.InterfaceString(claims["username"]),
		utils.InterfaceString(claims["role"]),
		permissions,
	)

	scope.IsImpersonated = utils.InterfaceBool(claims["is_impersonated"])
	scope.OriginalUserID = strings.TrimSpace(utils.InterfaceString(claims["original_user_id"]))
	scope.OriginalUsername = strings.TrimSpace(utils.InterfaceString(claims["original_username"]))
	scope.OriginalRole = strings.TrimSpace(utils.InterfaceString(claims["original_role"]))

	return scope
}

func WithContext(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey, scope)
}

func FromContext(ctx context.Context) Scope {
	if ctx == nil {
		return Scope{Permissions: map[string]struct{}{}}
	}

	scope, _ := ctx.Value(scopeContextKey).(Scope)
	if scope.Permissions == nil {
		scope.Permissions = map[string]struct{}{}
	}

	return scope
}

func PermissionKey(resource, action string) string {
	resourcePart := utils.NormalizeKey(resource)
	actionPart := utils.NormalizeKey(action)
	if resourcePart == "" || actionPart == "" {
		return ""
	}

	return resourcePart + ":" + actionPart
}

func (s Scope) Has(resource, action string) bool {
	if strings.TrimSpace(s.Role) == utils.RoleSuperAdmin {
		return true
	}

	_, ok := s.Permissions[PermissionKey(resource, action)]
	return ok
}

func (s Scope) ActorUserID() string {
	if s.IsImpersonated && strings.TrimSpace(s.OriginalUserID) != "" {
		return strings.TrimSpace(s.OriginalUserID)
	}
	return strings.TrimSpace(s.UserID)
}

func (s Scope) ActorRole() string {
	if s.IsImpersonated && strings.TrimSpace(s.OriginalRole) != "" {
		return strings.TrimSpace(s.OriginalRole)
	}
	return strings.TrimSpace(s.Role)
}

func normalizePermissionKey(permission string) string {
	parts := strings.SplitN(strings.TrimSpace(permission), ":", 2)
	if len(parts) != 2 {
		return ""
	}

	return PermissionKey(parts[0], parts[1])
}
