package serviceuser

import (
	"context"
	"errors"
	"starter-kit/internal/authscope"
	domainauth "starter-kit/internal/domain/auth"
	domainpermission "starter-kit/internal/domain/permission"
	domainrole "starter-kit/internal/domain/role"
	domainuser "starter-kit/internal/domain/user"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"starter-kit/utils"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func authContext(userID, username, role string, permissions ...string) context.Context {
	return authscope.WithContext(context.Background(), authscope.New(userID, username, role, permissions))
}

func impersonatedAuthContext(userID, username, role, originalUserID, originalUsername, originalRole string, permissions ...string) context.Context {
	scope := authscope.New(userID, username, role, permissions)
	scope.IsImpersonated = true
	scope.OriginalUserID = originalUserID
	scope.OriginalUsername = originalUsername
	scope.OriginalRole = originalRole
	return authscope.WithContext(context.Background(), scope)
}

type userRepoMock struct {
	user      domainuser.Users
	usersByID map[string]domainuser.Users
	users     []domainuser.Users
	updated   domainuser.Users
	deletedID string
	emailUser domainuser.Users
	emailErr  error
	phoneUser domainuser.Users
	phoneErr  error
	storeErr  error
	updateErr error
	deleteErr error
	getAllErr error
}

func (m *userRepoMock) Store(ctx context.Context, data domainuser.Users) error {
	m.user = data
	return m.storeErr
}
func (m *userRepoMock) GetByID(ctx context.Context, id string) (domainuser.Users, error) {
	if m.usersByID != nil {
		user, ok := m.usersByID[id]
		if !ok {
			return domainuser.Users{}, errors.New("not found")
		}
		return user, nil
	}
	return m.user, nil
}
func (m *userRepoMock) GetAll(ctx context.Context, params filter.BaseParams) ([]domainuser.Users, int64, error) {
	if m.getAllErr != nil {
		return nil, 0, m.getAllErr
	}
	return append([]domainuser.Users{}, m.users...), int64(len(m.users)), nil
}
func (m *userRepoMock) Update(ctx context.Context, data domainuser.Users) error {
	m.updated = data
	m.user = data
	return m.updateErr
}
func (m *userRepoMock) Delete(ctx context.Context, id string) error {
	m.deletedID = id
	return m.deleteErr
}
func (m *userRepoMock) SoftDelete(ctx context.Context, id, deletedBy string) error {
	m.deletedID = id
	return m.deleteErr
}
func (m *userRepoMock) GetByEmail(ctx context.Context, email string) (domainuser.Users, error) {
	if m.emailErr != nil {
		return domainuser.Users{}, m.emailErr
	}
	return m.emailUser, nil
}
func (m *userRepoMock) GetByPhone(ctx context.Context, phone string) (domainuser.Users, error) {
	if m.phoneErr != nil {
		return domainuser.Users{}, m.phoneErr
	}
	return m.phoneUser, nil
}

type authRepoMock struct {
	err       error
	existsErr error
	tokens    map[string]struct{}
	stored    domainauth.Blacklist
}

func (m *authRepoMock) Store(ctx context.Context, data domainauth.Blacklist) error {
	if m.err != nil {
		return m.err
	}
	m.stored = data
	if m.tokens == nil {
		m.tokens = make(map[string]struct{})
	}
	m.tokens[data.Token] = struct{}{}
	return nil
}
func (m *authRepoMock) GetByToken(ctx context.Context, token string) (domainauth.Blacklist, error) {
	return domainauth.Blacklist{}, nil
}
func (m *authRepoMock) ExistsByToken(ctx context.Context, token string) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}
	_, ok := m.tokens[token]
	return ok, nil
}
func (m *authRepoMock) DeleteExpired(ctx context.Context, now time.Time) error { return nil }

type roleRepoUserMock struct {
	roles map[string]domainrole.Role
}

func (m *roleRepoUserMock) Store(ctx context.Context, data domainrole.Role) error { return nil }
func (m *roleRepoUserMock) GetByID(ctx context.Context, id string) (domainrole.Role, error) {
	return domainrole.Role{}, errors.New("not implemented")
}
func (m *roleRepoUserMock) GetAll(ctx context.Context, params filter.BaseParams) ([]domainrole.Role, int64, error) {
	return nil, 0, nil
}
func (m *roleRepoUserMock) Update(ctx context.Context, data domainrole.Role) error { return nil }
func (m *roleRepoUserMock) Delete(ctx context.Context, id string) error            { return nil }
func (m *roleRepoUserMock) SoftDelete(ctx context.Context, id, deletedBy string) error {
	return nil
}
func (m *roleRepoUserMock) GetByName(ctx context.Context, name string) (domainrole.Role, error) {
	role, ok := m.roles[name]
	if !ok {
		return domainrole.Role{}, errors.New("not found")
	}
	return role, nil
}
func (m *roleRepoUserMock) AssignPermissions(ctx context.Context, roleId string, permissionIds []string) error {
	return nil
}
func (m *roleRepoUserMock) RemovePermissions(ctx context.Context, roleId string, permissionIds []string) error {
	return nil
}
func (m *roleRepoUserMock) GetRolePermissions(ctx context.Context, roleId string) ([]string, error) {
	return nil, nil
}
func (m *roleRepoUserMock) AssignMenus(ctx context.Context, roleId string, menuIds []string) error {
	return nil
}
func (m *roleRepoUserMock) RemoveMenus(ctx context.Context, roleId string, menuIds []string) error {
	return nil
}
func (m *roleRepoUserMock) GetRoleMenus(ctx context.Context, roleId string) ([]string, error) {
	return nil, nil
}

type permissionRepoUserMock struct {
	userPermissions []domainpermission.Permission
	err             error
}

func (m *permissionRepoUserMock) Store(ctx context.Context, data domainpermission.Permission) error {
	return nil
}
func (m *permissionRepoUserMock) GetByID(ctx context.Context, id string) (domainpermission.Permission, error) {
	return domainpermission.Permission{}, errors.New("not implemented")
}
func (m *permissionRepoUserMock) GetAll(ctx context.Context, params filter.BaseParams) ([]domainpermission.Permission, int64, error) {
	return nil, 0, nil
}
func (m *permissionRepoUserMock) Update(ctx context.Context, data domainpermission.Permission) error {
	return nil
}
func (m *permissionRepoUserMock) Delete(ctx context.Context, id string) error { return nil }
func (m *permissionRepoUserMock) SoftDelete(ctx context.Context, id, deletedBy string) error {
	return nil
}
func (m *permissionRepoUserMock) GetByName(ctx context.Context, name string) (domainpermission.Permission, error) {
	return domainpermission.Permission{}, errors.New("not implemented")
}
func (m *permissionRepoUserMock) GetByResource(ctx context.Context, resource string) ([]domainpermission.Permission, error) {
	return nil, nil
}
func (m *permissionRepoUserMock) GetUserPermissions(ctx context.Context, userId string) ([]domainpermission.Permission, error) {
	if m.err != nil {
		return nil, m.err
	}
	return append([]domainpermission.Permission{}, m.userPermissions...), nil
}

func TestAdminCreateUserRequiresAssignRolePermissionForNonViewer(t *testing.T) {
	service := &ServiceUser{
		UserRepo:      &userRepoMock{},
		BlacklistRepo: &authRepoMock{},
		RoleRepo: &roleRepoUserMock{roles: map[string]domainrole.Role{
			utils.RoleStaff: {Id: "role-staff", Name: utils.RoleStaff},
		}},
		PermissionRepo: &permissionRepoUserMock{},
	}

	_, err := service.AdminCreateUser(authContext("creator-1", "Admin User", utils.RoleAdmin), dto.AdminCreateUser{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "Password1!",
		Role:     utils.RoleStaff,
	})
	if err == nil || err.Error() != "access denied: missing permission users:assign_role" {
		t.Fatalf("expected assign_role access error, got %v", err)
	}
}

func TestUpdateRequiresAssignRolePermissionWhenChangingRole(t *testing.T) {
	service := &ServiceUser{
		UserRepo: &userRepoMock{
			user: domainuser.Users{Id: "user-1", Role: utils.RoleViewer},
		},
		BlacklistRepo: &authRepoMock{},
		RoleRepo: &roleRepoUserMock{roles: map[string]domainrole.Role{
			utils.RoleStaff: {Id: "role-staff", Name: utils.RoleStaff},
		}},
		PermissionRepo: &permissionRepoUserMock{},
	}

	_, err := service.Update(authContext("editor-1", "Editor User", utils.RoleAdmin), "user-1", dto.UserUpdate{Role: utils.RoleStaff})
	if err == nil || err.Error() != "access denied: missing permission users:assign_role" {
		t.Fatalf("expected assign_role access error, got %v", err)
	}
}

func TestUpdateRejectsSuperadminAssignmentForNonSuperadmin(t *testing.T) {
	service := &ServiceUser{
		UserRepo: &userRepoMock{
			user: domainuser.Users{Id: "user-1", Role: utils.RoleViewer},
		},
		BlacklistRepo: &authRepoMock{},
		RoleRepo: &roleRepoUserMock{roles: map[string]domainrole.Role{
			utils.RoleSuperAdmin: {Id: "role-superadmin", Name: utils.RoleSuperAdmin},
		}},
		PermissionRepo: &permissionRepoUserMock{
			userPermissions: []domainpermission.Permission{{Resource: "users", Action: "assign_role"}},
		},
	}

	_, err := service.Update(authContext("editor-1", "Editor User", utils.RoleAdmin, "users:assign_role"), "user-1", dto.UserUpdate{Role: utils.RoleSuperAdmin})
	if err == nil || err.Error() != "cannot assign superadmin role" {
		t.Fatalf("expected superadmin assignment error, got %v", err)
	}
}

func TestRegisterUserNormalizesEmailToLowercase(t *testing.T) {
	service := &ServiceUser{
		UserRepo:      &userRepoMock{},
		BlacklistRepo: &authRepoMock{},
		RoleRepo: &roleRepoUserMock{roles: map[string]domainrole.Role{
			utils.RoleViewer: {Id: "role-viewer", Name: utils.RoleViewer},
		}},
		PermissionRepo: &permissionRepoUserMock{},
	}

	user, err := service.RegisterUser(context.Background(), dto.UserRegister{
		Name:     "Jane Doe",
		Email:    "Jane.Doe@Example.COM",
		Phone:    "08123456789",
		Password: "Password1!",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if user.Email != "jane.doe@example.com" {
		t.Fatalf("expected normalized lowercase email, got %s", user.Email)
	}
}

func TestAdminCreateUserNormalizesEmailToLowercase(t *testing.T) {
	service := &ServiceUser{
		UserRepo:      &userRepoMock{},
		BlacklistRepo: &authRepoMock{},
		RoleRepo: &roleRepoUserMock{roles: map[string]domainrole.Role{
			utils.RoleStaff: {Id: "role-staff", Name: utils.RoleStaff},
		}},
		PermissionRepo: &permissionRepoUserMock{
			userPermissions: []domainpermission.Permission{{Resource: "users", Action: "assign_role"}},
		},
	}

	user, err := service.AdminCreateUser(authContext("creator-1", "Admin User", utils.RoleAdmin, "users:assign_role"), dto.AdminCreateUser{
		Name:     "Jane Doe",
		Email:    "Jane.Doe@Example.COM",
		Phone:    "08123456789",
		Password: "Password1!",
		Role:     utils.RoleStaff,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if user.Email != "jane.doe@example.com" {
		t.Fatalf("expected normalized lowercase email, got %s", user.Email)
	}
}

func TestUpdateNormalizesEmailToLowercase(t *testing.T) {
	service := &ServiceUser{
		UserRepo: &userRepoMock{
			user: domainuser.Users{Id: "user-1", Role: utils.RoleViewer, Email: "old@example.com"},
		},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	user, err := service.Update(authContext("user-1", "Jane Doe", utils.RoleViewer), "user-1", dto.UserUpdate{
		Email: "Jane.Doe@Example.COM",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if user.Email != "jane.doe@example.com" {
		t.Fatalf("expected normalized lowercase email, got %s", user.Email)
	}
}

func TestLoginUserAcceptsEmailIdentifier(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("Password1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	service := &ServiceUser{
		UserRepo: &userRepoMock{
			emailUser: domainuser.Users{
				Id:       "user-1",
				Name:     "Jane Doe",
				Email:    "jane.doe@example.com",
				Password: string(hashedPassword),
				Role:     utils.RoleViewer,
			},
		},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	token, err := service.LoginUser(context.Background(), dto.Login{
		Identifier: "Jane.Doe@Example.COM",
		Password:   "Password1!",
	}, "log-1", dto.LoginMetadata{IP: "127.0.0.1", UserAgent: "test-agent"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if service.UserRepo.(*userRepoMock).updated.LastLoginAt == nil {
		t.Fatal("expected last login timestamp to be updated")
	}
}

func TestLoginUserAcceptsPhoneIdentifier(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("Password1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	service := &ServiceUser{
		UserRepo: &userRepoMock{
			phoneUser: domainuser.Users{
				Id:       "user-1",
				Name:     "Jane Doe",
				Phone:    "628123456789",
				Password: string(hashedPassword),
				Role:     utils.RoleViewer,
			},
		},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	token, err := service.LoginUser(context.Background(), dto.Login{
		Identifier: "08123456789",
		Password:   "Password1!",
	}, "log-1", dto.LoginMetadata{IP: "127.0.0.1", UserAgent: "test-agent"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestLoginUserRejectsInvalidRandomIdentifier(t *testing.T) {
	service := &ServiceUser{
		UserRepo:       &userRepoMock{},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	_, err := service.LoginUser(context.Background(), dto.Login{
		Identifier: "randomtext",
		Password:   "Password1!",
	}, "log-1", dto.LoginMetadata{})
	if err == nil || err.Error() != "identifier must be a valid email or phone number" {
		t.Fatalf("expected invalid identifier error, got %v", err)
	}
}

func TestResolveLoginIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		req     dto.Login
		want    string
		wantErr string
	}{
		{
			name: "email identifier",
			req:  dto.Login{Identifier: " Jane.Doe@Example.COM "},
			want: "jane.doe@example.com",
		},
		{
			name: "email fallback",
			req:  dto.Login{Email: " Jane.Doe@Example.COM "},
			want: "jane.doe@example.com",
		},
		{
			name: "phone identifier",
			req:  dto.Login{Identifier: "0812-3456-7890"},
			want: "6281234567890",
		},
		{
			name:    "empty identifier",
			req:     dto.Login{},
			wantErr: "identifier or email is required",
		},
		{
			name:    "invalid identifier",
			req:     dto.Login{Identifier: "randomtext"},
			wantErr: "identifier must be a valid email or phone number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveLoginIdentifier(tt.req)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected success, got %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected identifier %q, got %q", tt.want, got)
			}
		})
	}
}

func TestImpersonateUserGeneratesTokenWithOriginalUserClaims(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	service := &ServiceUser{
		UserRepo: &userRepoMock{
			usersByID: map[string]domainuser.Users{
				"target-1": {
					Id:    "target-1",
					Name:  "Target User",
					Role:  utils.RoleStaff,
					Email: "target@example.com",
				},
			},
		},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	token, err := service.ImpersonateUser(authContext("admin-1", "Admin User", utils.RoleAdmin), "target-1", "log-1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	claims, err := utils.JwtClaim(token)
	if err != nil {
		t.Fatalf("failed to parse token claims: %v", err)
	}

	if claims["user_id"] != "target-1" {
		t.Fatalf("expected target user id in claims, got %v", claims["user_id"])
	}
	if claims["is_impersonated"] != true {
		t.Fatalf("expected impersonation flag in claims, got %v", claims["is_impersonated"])
	}
	if claims["original_user_id"] != "admin-1" {
		t.Fatalf("expected original user id in claims, got %v", claims["original_user_id"])
	}
}

func TestImpersonateUserRejectsSuperadminTargetForNonSuperadmin(t *testing.T) {
	service := &ServiceUser{
		UserRepo: &userRepoMock{
			usersByID: map[string]domainuser.Users{
				"target-1": {
					Id:   "target-1",
					Name: "Superadmin User",
					Role: utils.RoleSuperAdmin,
				},
			},
		},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	_, err := service.ImpersonateUser(authContext("admin-1", "Admin User", utils.RoleAdmin), "target-1", "log-1")
	if err == nil || err.Error() != "cannot impersonate superadmin users" {
		t.Fatalf("expected superadmin impersonation error, got %v", err)
	}
}

func TestStopImpersonationReturnsOriginalUserToken(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")

	service := &ServiceUser{
		UserRepo: &userRepoMock{
			usersByID: map[string]domainuser.Users{
				"admin-1": {
					Id:   "admin-1",
					Name: "Admin User",
					Role: utils.RoleAdmin,
				},
			},
		},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	token, err := service.StopImpersonation(impersonatedAuthContext("target-1", "Target User", utils.RoleStaff, "admin-1", "Admin User", utils.RoleAdmin), "log-1")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	claims, err := utils.JwtClaim(token)
	if err != nil {
		t.Fatalf("failed to parse token claims: %v", err)
	}

	if claims["user_id"] != "admin-1" {
		t.Fatalf("expected original user id in claims, got %v", claims["user_id"])
	}
	if _, exists := claims["is_impersonated"]; exists {
		t.Fatalf("expected impersonation flag to be absent after stop, got %v", claims["is_impersonated"])
	}
}

func TestLoginWithGoogleReturnsExistingUser(t *testing.T) {
	originalVerifier := googleIDTokenVerifier
	googleIDTokenVerifier = func(_ context.Context, idToken string) (googleTokenInfo, error) {
		return googleTokenInfo{
			Email:         "Jane.Doe@Example.COM",
			EmailVerified: "true",
			Subject:       "google-sub-1",
			Name:          "Jane Doe",
			Audience:      "client-id",
		}, nil
	}
	defer func() { googleIDTokenVerifier = originalVerifier }()

	service := &ServiceUser{
		UserRepo: &userRepoMock{
			emailUser: domainuser.Users{
				Id:    "user-1",
				Name:  "Jane Doe",
				Email: "jane.doe@example.com",
				Role:  utils.RoleViewer,
			},
		},
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	user, isNewUser, err := service.LoginWithGoogle(context.Background(), dto.GoogleLogin{IDToken: "token"}, dto.LoginMetadata{IP: "127.0.0.1", UserAgent: "test-agent"}, true)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if isNewUser {
		t.Fatal("expected existing user login, got isNewUser=true")
	}
	if user.Id != "user-1" {
		t.Fatalf("expected existing user, got %+v", user)
	}
}

func TestLoginWithGoogleCreatesNewViewerUser(t *testing.T) {
	originalVerifier := googleIDTokenVerifier
	googleIDTokenVerifier = func(_ context.Context, idToken string) (googleTokenInfo, error) {
		return googleTokenInfo{
			Email:         "New.User@Example.COM",
			EmailVerified: "true",
			Subject:       "google-sub-2",
			Name:          "new user",
			Audience:      "client-id",
		}, nil
	}
	defer func() { googleIDTokenVerifier = originalVerifier }()

	userRepo := &userRepoMock{emailErr: gorm.ErrRecordNotFound}
	service := &ServiceUser{
		UserRepo:      userRepo,
		BlacklistRepo: &authRepoMock{},
		RoleRepo: &roleRepoUserMock{roles: map[string]domainrole.Role{
			utils.RoleViewer: {Id: "role-viewer", Name: utils.RoleViewer},
		}},
		PermissionRepo: &permissionRepoUserMock{},
	}

	user, isNewUser, err := service.LoginWithGoogle(context.Background(), dto.GoogleLogin{IDToken: "token"}, dto.LoginMetadata{IP: "127.0.0.1", UserAgent: "test-agent"}, true)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !isNewUser {
		t.Fatal("expected new user registration, got isNewUser=false")
	}
	if user.Email != "new.user@example.com" {
		t.Fatalf("expected normalized email, got %s", user.Email)
	}
	if user.Role != utils.RoleViewer {
		t.Fatalf("expected viewer role, got %s", user.Role)
	}
	if userRepo.user.Password == "" {
		t.Fatal("expected generated password hash for google user")
	}
}

func TestLoginWithGoogleRejectsNewUserWhenPublicRegistrationDisabled(t *testing.T) {
	originalVerifier := googleIDTokenVerifier
	googleIDTokenVerifier = func(_ context.Context, idToken string) (googleTokenInfo, error) {
		return googleTokenInfo{
			Email:         "new.user@example.com",
			EmailVerified: "true",
			Subject:       "google-sub-3",
			Name:          "new user",
			Audience:      "client-id",
		}, nil
	}
	defer func() { googleIDTokenVerifier = originalVerifier }()

	userRepo := &userRepoMock{emailErr: gorm.ErrRecordNotFound}
	service := &ServiceUser{
		UserRepo:      userRepo,
		BlacklistRepo: &authRepoMock{},
		RoleRepo: &roleRepoUserMock{roles: map[string]domainrole.Role{
			utils.RoleViewer: {Id: "role-viewer", Name: utils.RoleViewer},
		}},
		PermissionRepo: &permissionRepoUserMock{},
	}

	_, _, err := service.LoginWithGoogle(
		context.Background(),
		dto.GoogleLogin{IDToken: "token"},
		dto.LoginMetadata{IP: "127.0.0.1", UserAgent: "test-agent"},
		false,
	)
	if !errors.Is(err, ErrPublicRegistrationDisabled) {
		t.Fatalf("expected ErrPublicRegistrationDisabled, got %v", err)
	}
	if userRepo.user.Id != "" {
		t.Fatalf("expected no user to be created, got %+v", userRepo.user)
	}
}

func TestGoogleTokenVerifierLocalValidationBranches(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_IDS", "")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	if _, err := verifyGoogleIDToken(context.Background(), " "); !errors.Is(err, ErrGoogleTokenInvalid) {
		t.Fatalf("expected invalid token for empty input, got %v", err)
	}
	if _, err := verifyGoogleIDToken(context.Background(), "token"); !errors.Is(err, ErrGoogleNotConfigured) {
		t.Fatalf("expected not configured without audiences, got %v", err)
	}

	t.Setenv("GOOGLE_CLIENT_IDS", "client-a, client-b,,")
	t.Setenv("GOOGLE_CLIENT_ID", "client-c")
	audiences := googleAllowedAudiences()
	for _, audience := range []string{"client-a", "client-b", "client-c"} {
		if _, ok := audiences[audience]; !ok {
			t.Fatalf("expected audience %q in %#v", audience, audiences)
		}
	}
}

func TestResetPasswordByEmailNormalizesEmailAndUpdatesPassword(t *testing.T) {
	oldPassword, err := bcrypt.GenerateFromPassword([]byte("OldPassword1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash old password: %v", err)
	}

	userRepo := &userRepoMock{
		emailUser: domainuser.Users{
			Id:       "user-1",
			Email:    "jane.doe@example.com",
			Password: string(oldPassword),
			Role:     utils.RoleViewer,
		},
	}
	service := &ServiceUser{
		UserRepo:       userRepo,
		BlacklistRepo:  &authRepoMock{},
		RoleRepo:       &roleRepoUserMock{},
		PermissionRepo: &permissionRepoUserMock{},
	}

	if err := service.ResetPasswordByEmail(context.Background(), " Jane.Doe@Example.COM ", "NewPassword1!"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if userRepo.updated.Email != "jane.doe@example.com" {
		t.Fatalf("expected existing normalized email to be updated, got %s", userRepo.updated.Email)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(userRepo.updated.Password), []byte("NewPassword1!")); err != nil {
		t.Fatalf("expected password to be updated, got %v", err)
	}
}

func TestUserServicePassThroughAndFilteringMethods(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")
	userRepo := &userRepoMock{
		user:      domainuser.Users{Id: "user-1", Name: "Jane", Email: "jane@example.com", Phone: "628123456789", Role: utils.RoleViewer},
		emailUser: domainuser.Users{Id: "user-1", Email: "jane@example.com"},
		phoneUser: domainuser.Users{Id: "user-1", Phone: "628123456789"},
		users: []domainuser.Users{
			{Id: "user-1", Role: utils.RoleViewer},
			{Id: "user-2", Role: utils.RoleSuperAdmin},
		},
	}
	service := NewUserService(userRepo, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{
		userPermissions: []domainpermission.Permission{{Name: "users.list", Resource: "users", Action: "list"}},
	})

	if got, err := service.GetUserById(context.Background(), "user-1"); err != nil || got.Id != "user-1" {
		t.Fatalf("get by id: user=%+v err=%v", got, err)
	}
	if got, err := service.GetUserByEmail(context.Background(), "jane@example.com"); err != nil || got.Id != "user-1" {
		t.Fatalf("get by email: user=%+v err=%v", got, err)
	}
	if got, err := service.GetUserByPhone(context.Background(), "628123456789"); err != nil || got.Id != "user-1" {
		t.Fatalf("get by phone: user=%+v err=%v", got, err)
	}
	if got, err := service.GetUserByAuth(context.Background(), "user-1"); err != nil || got["id"] != "user-1" {
		t.Fatalf("get by auth: user=%+v err=%v", got, err)
	}

	users, total, err := service.GetAllUsers(authContext("viewer-1", "Viewer", utils.RoleViewer), filter.BaseParams{})
	if err != nil || total != 1 || len(users) != 1 || users[0].Role == utils.RoleSuperAdmin {
		t.Fatalf("expected superadmin filtered for non-superadmin, users=%+v total=%d err=%v", users, total, err)
	}
	users, total, err = service.GetAllUsers(authContext("root", "Root", utils.RoleSuperAdmin), filter.BaseParams{})
	if err != nil || total != 2 || len(users) != 2 {
		t.Fatalf("expected superadmin to see all users, users=%+v total=%d err=%v", users, total, err)
	}

	logoutToken, err := utils.GenerateJwt(&userRepo.user, "logout-test")
	if err != nil {
		t.Fatalf("generate logout token: %v", err)
	}
	if err := service.LogoutUser(context.Background(), logoutToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if err := service.Delete(context.Background(), "user-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if userRepo.deletedID != "user-1" {
		t.Fatalf("expected delete delegation, got %q", userRepo.deletedID)
	}
}

func TestChangePasswordAndForgotResetPasswordFlows(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")
	oldPassword, err := bcrypt.GenerateFromPassword([]byte("OldPassword1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userRepo := &userRepoMock{
		user:      domainuser.Users{Id: "user-1", Email: "jane@example.com", Password: string(oldPassword), Role: utils.RoleViewer},
		emailUser: domainuser.Users{Id: "user-1", Email: "jane@example.com", Password: string(oldPassword), Role: utils.RoleViewer},
	}
	authRepo := &authRepoMock{}
	service := NewUserService(userRepo, authRepo, &roleRepoUserMock{}, &permissionRepoUserMock{})

	changed, err := service.ChangePassword(context.Background(), "user-1", dto.ChangePassword{CurrentPassword: "OldPassword1!", NewPassword: "NewPassword1!"})
	if err != nil || changed.PasswordChangedAt == nil {
		t.Fatalf("change password: user=%+v err=%v", changed, err)
	}

	token, err := service.ForgotPassword(context.Background(), dto.ForgotPasswordRequest{Email: "jane@example.com"})
	if err != nil || token == "" {
		t.Fatalf("forgot password: token=%q err=%v", token, err)
	}
	if err := service.ResetPassword(context.Background(), dto.ResetPasswordRequest{Token: token, NewPassword: "AnotherPassword1!"}); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if _, ok := authRepo.tokens[token]; !ok {
		t.Fatalf("expected reset token to be blacklisted")
	}
	if authRepo.stored.ExpiresAt.IsZero() || time.Until(authRepo.stored.ExpiresAt) <= 0 {
		t.Fatalf("expected reset token blacklist expiry, got %v", authRepo.stored.ExpiresAt)
	}
	if err := service.ResetPassword(context.Background(), dto.ResetPasswordRequest{Token: token, NewPassword: "AnotherPassword1!"}); err == nil || err.Error() != "invalid or expired token" {
		t.Fatalf("expected used reset token to fail, got %v", err)
	}
}

func TestRegisterUserValidationAndRepositoryErrors(t *testing.T) {
	service := NewUserService(&userRepoMock{
		emailUser: domainuser.Users{Id: "existing-email"},
	}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err := service.RegisterUser(context.Background(), dto.UserRegister{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "Password1!",
	})
	if err == nil || err.Error() != "email already exists" {
		t.Fatalf("expected duplicate email error, got %v", err)
	}

	service = NewUserService(&userRepoMock{
		phoneUser: domainuser.Users{Id: "existing-phone"},
	}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.RegisterUser(context.Background(), dto.UserRegister{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "Password1!",
	})
	if err == nil || err.Error() != "phone number already exists" {
		t.Fatalf("expected duplicate phone error, got %v", err)
	}

	service = NewUserService(&userRepoMock{}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.RegisterUser(context.Background(), dto.UserRegister{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "weak",
	})
	if err == nil || err.Error() != "password must be at least 8 characters long" {
		t.Fatalf("expected weak password error, got %v", err)
	}

	service = NewUserService(&userRepoMock{storeErr: errors.New("insert failed")}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.RegisterUser(context.Background(), dto.UserRegister{
		Name:          "Jane Doe",
		Email:         "jane@example.com",
		Phone:         "08123456789",
		Password:      "Password1!",
		EmailVerified: true,
	})
	if err == nil || err.Error() != "insert failed" {
		t.Fatalf("expected store error, got %v", err)
	}
}

func TestAdminCreateUserValidationBranches(t *testing.T) {
	service := NewUserService(&userRepoMock{emailUser: domainuser.Users{Id: "existing-email"}}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err := service.AdminCreateUser(authContext("admin-1", "Admin", utils.RoleAdmin, "users:assign_role"), dto.AdminCreateUser{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "Password1!",
		Role:     utils.RoleViewer,
	})
	if err == nil || err.Error() != "email already exists" {
		t.Fatalf("expected duplicate email, got %v", err)
	}

	service = NewUserService(&userRepoMock{phoneUser: domainuser.Users{Id: "existing-phone"}}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.AdminCreateUser(authContext("admin-1", "Admin", utils.RoleAdmin, "users:assign_role"), dto.AdminCreateUser{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "Password1!",
		Role:     utils.RoleViewer,
	})
	if err == nil || err.Error() != "phone number already exists" {
		t.Fatalf("expected duplicate phone, got %v", err)
	}

	service = NewUserService(&userRepoMock{}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.AdminCreateUser(authContext("admin-1", "Admin", utils.RoleAdmin, "users:assign_role"), dto.AdminCreateUser{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "Password1!",
		Role:     utils.RoleSuperAdmin,
	})
	if err == nil || err.Error() != "only superadmin can create superadmin users" {
		t.Fatalf("expected superadmin creation guard, got %v", err)
	}

	service = NewUserService(&userRepoMock{}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{err: errors.New("permission lookup failed")})
	_, err = service.AdminCreateUser(authContext("admin-1", "Admin", utils.RoleAdmin), dto.AdminCreateUser{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "08123456789",
		Password: "Password1!",
		Role:     utils.RoleStaff,
	})
	if err == nil || err.Error() != "permission lookup failed" {
		t.Fatalf("expected permission repo error, got %v", err)
	}

	service = NewUserService(&userRepoMock{storeErr: errors.New("insert failed")}, &authRepoMock{}, &roleRepoUserMock{roles: map[string]domainrole.Role{
		utils.RoleViewer: {Id: "role-viewer", Name: utils.RoleViewer},
	}}, &permissionRepoUserMock{})
	_, err = service.AdminCreateUser(authContext("admin-1", "Admin", utils.RoleAdmin), dto.AdminCreateUser{
		Name:     "Jane Doe",
		Email:    "jane@example.com",
		Phone:    "",
		Password: "Password1!",
		Role:     utils.RoleViewer,
	})
	if err == nil || err.Error() != "insert failed" {
		t.Fatalf("expected store error, got %v", err)
	}
}

func TestUserServiceErrorBranches(t *testing.T) {
	t.Setenv("JWT_KEY", "test-secret-must-be-at-least-32-bytes")
	oldPassword, err := bcrypt.GenerateFromPassword([]byte("OldPassword1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	service := NewUserService(&userRepoMock{
		emailUser: domainuser.Users{Id: "user-1", Email: "jane@example.com", Password: string(oldPassword), Role: utils.RoleViewer},
		updateErr: errors.New("update failed"),
	}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.LoginUser(context.Background(), dto.Login{Identifier: "jane@example.com", Password: "OldPassword1!"}, "log-1", dto.LoginMetadata{})
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected login update error, got %v", err)
	}

	service = NewUserService(&userRepoMock{
		user: domainuser.Users{Id: "root", Role: utils.RoleSuperAdmin},
	}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.Update(authContext("admin-1", "Admin", utils.RoleAdmin), "root", dto.UserUpdate{Name: "Root"})
	if err == nil || err.Error() != "cannot modify superadmin users" {
		t.Fatalf("expected superadmin modify guard, got %v", err)
	}

	service = NewUserService(&userRepoMock{user: domainuser.Users{Id: "user-1", Role: utils.RoleViewer}}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.Update(authContext("admin-1", "Admin", utils.RoleAdmin), "user-1", dto.UserUpdate{Role: utils.RoleStaff})
	if err == nil || err.Error() != "access denied: missing permission users:assign_role" {
		t.Fatalf("expected role assignment permission guard, got %v", err)
	}

	service = NewUserService(&userRepoMock{user: domainuser.Users{Id: "user-1", Role: utils.RoleViewer}, updateErr: errors.New("update failed")}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.Update(authContext("user-1", "Jane", utils.RoleViewer), "user-1", dto.UserUpdate{Name: "Jane Updated"})
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected update error, got %v", err)
	}

	service = NewUserService(&userRepoMock{user: domainuser.Users{Id: "user-1", Password: string(oldPassword)}, updateErr: errors.New("update failed")}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	_, err = service.ChangePassword(context.Background(), "user-1", dto.ChangePassword{CurrentPassword: "OldPassword1!", NewPassword: "NewPassword1!"})
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected change password update error, got %v", err)
	}

	_, err = service.ChangePassword(context.Background(), "user-1", dto.ChangePassword{CurrentPassword: "SamePassword1!", NewPassword: "SamePassword1!"})
	if err == nil || err.Error() != "new password must be different from current password" {
		t.Fatalf("expected same password error, got %v", err)
	}

	service = NewUserService(&userRepoMock{emailErr: gorm.ErrRecordNotFound}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	token, err := service.ForgotPassword(context.Background(), dto.ForgotPasswordRequest{Email: "missing@example.com"})
	if err != nil || token != "" {
		t.Fatalf("expected missing forgot password to return empty success, token=%q err=%v", token, err)
	}

	service = NewUserService(&userRepoMock{}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	if err := service.ResetPassword(context.Background(), dto.ResetPasswordRequest{Token: "bad-token", NewPassword: "Password1!"}); err == nil || err.Error() != "invalid or expired token" {
		t.Fatalf("expected invalid token error, got %v", err)
	}

	resetUser := domainuser.Users{Id: "user-1", Email: "jane@example.com", Password: string(oldPassword), Role: utils.RoleViewer}
	resetToken, err := utils.GeneratePasswordResetJwt(&resetUser, "reset_password")
	if err != nil {
		t.Fatalf("generate reset token: %v", err)
	}
	userRepo := &userRepoMock{user: resetUser}
	service = NewUserService(userRepo, &authRepoMock{err: errors.New("blacklist failed")}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	if err := service.ResetPassword(context.Background(), dto.ResetPasswordRequest{Token: resetToken, NewPassword: "Password1!"}); err == nil || err.Error() != "blacklist failed" {
		t.Fatalf("expected reset token blacklist error, got %v", err)
	}
	if userRepo.updated.Id != "" {
		t.Fatalf("expected password update to be skipped when reset token blacklist fails")
	}

	service = NewUserService(&userRepoMock{emailErr: gorm.ErrRecordNotFound}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	if err := service.ResetPasswordByEmail(context.Background(), "missing@example.com", "Password1!"); err == nil || err.Error() != "user not found" {
		t.Fatalf("expected reset by email missing user, got %v", err)
	}

	logoutToken, err := utils.GenerateJwt(&resetUser, "logout-test")
	if err != nil {
		t.Fatalf("generate logout token: %v", err)
	}
	service = NewUserService(&userRepoMock{}, &authRepoMock{err: errors.New("blacklist failed")}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	if err := service.LogoutUser(context.Background(), logoutToken); err == nil || err.Error() != "blacklist failed" {
		t.Fatalf("expected logout store error, got %v", err)
	}

	service = NewUserService(&userRepoMock{getAllErr: errors.New("list failed")}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	if _, _, err := service.GetAllUsers(context.Background(), filter.BaseParams{}); err == nil || err.Error() != "list failed" {
		t.Fatalf("expected get all error, got %v", err)
	}
}

func TestImpersonationValidationBranches(t *testing.T) {
	service := NewUserService(&userRepoMock{}, &authRepoMock{}, &roleRepoUserMock{}, &permissionRepoUserMock{})
	if _, err := service.ImpersonateUser(impersonatedAuthContext("target-1", "Target", utils.RoleViewer, "admin-1", "Admin", utils.RoleAdmin), "target-2", "log-1"); err == nil || err.Error() != "cannot start nested impersonation" {
		t.Fatalf("expected nested impersonation error, got %v", err)
	}
	if _, err := service.ImpersonateUser(authContext("user-1", "Jane", utils.RoleViewer), "", "log-1"); err == nil || err.Error() != "target user id is required" {
		t.Fatalf("expected missing target error, got %v", err)
	}
	if _, err := service.ImpersonateUser(authContext("user-1", "Jane", utils.RoleViewer), "user-1", "log-1"); err == nil || err.Error() != "cannot impersonate your own account" {
		t.Fatalf("expected self impersonation error, got %v", err)
	}

	if _, err := service.StopImpersonation(authContext("user-1", "Jane", utils.RoleViewer), "log-1"); err == nil || err.Error() != "original user id is required" {
		t.Fatalf("expected missing original user error, got %v", err)
	}
	if _, err := service.StopImpersonation(impersonatedAuthContext("user-1", "Jane", utils.RoleViewer, "user-1", "Jane", utils.RoleViewer), "log-1"); err == nil || err.Error() != "current session is not impersonated" {
		t.Fatalf("expected not impersonated error, got %v", err)
	}
}
