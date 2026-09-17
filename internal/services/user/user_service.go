package serviceuser

import (
	"context"
	"errors"
	"starter-kit/internal/authscope"
	permissioncache "starter-kit/internal/cache/permission"
	domainauth "starter-kit/internal/domain/auth"
	domainuser "starter-kit/internal/domain/user"
	"starter-kit/internal/dto"
	interfaceauth "starter-kit/internal/interfaces/auth"
	interfacepermission "starter-kit/internal/interfaces/permission"
	interfacerole "starter-kit/internal/interfaces/role"
	interfaceuser "starter-kit/internal/interfaces/user"
	serviceshared "starter-kit/internal/services/shared"
	"starter-kit/pkg/filter"
	"starter-kit/utils"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type ServiceUser struct {
	UserRepo        interfaceuser.RepoUserInterface
	BlacklistRepo   interfaceauth.RepoAuthInterface
	RoleRepo        interfacerole.RepoRoleInterface
	PermissionRepo  interfacepermission.RepoPermissionInterface
	PermissionCache permissioncache.Invalidator
}

func NewUserService(userRepo interfaceuser.RepoUserInterface, blacklistRepo interfaceauth.RepoAuthInterface, roleRepo interfacerole.RepoRoleInterface, permissionRepo interfacepermission.RepoPermissionInterface, invalidators ...permissioncache.Invalidator) *ServiceUser {
	service := &ServiceUser{
		UserRepo:       userRepo,
		BlacklistRepo:  blacklistRepo,
		RoleRepo:       roleRepo,
		PermissionRepo: permissionRepo,
	}
	if len(invalidators) > 0 {
		service.PermissionCache = invalidators[0]
	}
	return service
}

func (s *ServiceUser) RegisterUser(ctx context.Context, req dto.UserRegister) (domainuser.Users, error) {
	phone := utils.NormalizePhoneTo62(req.Phone)
	email := utils.SanitizeEmail(req.Email)

	data, _ := s.UserRepo.GetByEmail(ctx, email)
	if data.Id != "" {
		return domainuser.Users{}, errors.New("email already exists")
	}

	phoneData, _ := s.UserRepo.GetByPhone(ctx, phone)
	if phoneData.Id != "" {
		return domainuser.Users{}, errors.New("phone number already exists")
	}

	if err := ValidatePasswordStrength(req.Password); err != nil {
		return domainuser.Users{}, err
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return domainuser.Users{}, err
	}

	// SECURITY: Public registration always uses vendor role
	// This prevents privilege escalation through request manipulation
	roleName := utils.RoleViewer

	roleId, _ := findRoleIDByName(ctx, s.RoleRepo, roleName)
	var emailVerifiedAt *time.Time
	if req.EmailVerified {
		emailVerifiedAt = new(time.Now())
	}

	data = domainuser.Users{
		Id:                utils.CreateUUID(),
		Name:              utils.TitleCase(utils.StripHTML(req.Name)),
		Phone:             phone,
		Email:             email,
		Password:          string(hashedPwd),
		Role:              roleName,
		RoleId:            roleId,
		EmailVerifiedAt:   emailVerifiedAt,
		PasswordChangedAt: new(time.Now()),
		LoginProvider:     "local",
		Metadata:          map[string]any{},
		CreatedAt:         time.Now(),
	}

	if err = s.UserRepo.Store(ctx, data); err != nil {
		return domainuser.Users{}, err
	}

	return data, nil
}

func (s *ServiceUser) AdminCreateUser(ctx context.Context, req dto.AdminCreateUser) (domainuser.Users, error) {
	scope := authscope.FromContext(ctx)
	phone := utils.NormalizePhoneTo62(req.Phone)
	email := utils.SanitizeEmail(req.Email)

	data, _ := s.UserRepo.GetByEmail(ctx, email)
	if data.Id != "" {
		return domainuser.Users{}, errors.New("email already exists")
	}

	if phone != "" {
		phoneData, _ := s.UserRepo.GetByPhone(ctx, phone)
		if phoneData.Id != "" {
			return domainuser.Users{}, errors.New("phone number already exists")
		}
	}

	if err := ValidatePasswordStrength(req.Password); err != nil {
		return domainuser.Users{}, err
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return domainuser.Users{}, err
	}

	roleName := utils.NormalizeKey(req.Role)
	canAssignRole, err := serviceshared.HasPermission(ctx, s.PermissionRepo, "users", "assign_role")
	if err != nil {
		return domainuser.Users{}, err
	}

	if roleName != utils.RoleViewer && !canAssignRole {
		return domainuser.Users{}, errors.New("access denied: missing permission users:assign_role")
	}

	if roleName == utils.RoleSuperAdmin && scope.Role != utils.RoleSuperAdmin {
		return domainuser.Users{}, errors.New("only superadmin can create superadmin users")
	}

	roleId, ok := findRoleIDByName(ctx, s.RoleRepo, roleName)
	if !ok {
		return domainuser.Users{}, errors.New("invalid role: " + roleName)
	}

	data = domainuser.Users{
		Id:                utils.CreateUUID(),
		Name:              utils.TitleCase(utils.StripHTML(req.Name)),
		Phone:             phone,
		Email:             email,
		Password:          string(hashedPwd),
		Role:              roleName,
		RoleId:            roleId,
		PasswordChangedAt: new(time.Now()),
		LoginProvider:     "local",
		Metadata:          map[string]any{},
		CreatedAt:         time.Now(),
	}

	if err = s.UserRepo.Store(ctx, data); err != nil {
		return domainuser.Users{}, err
	}

	return data, nil
}

func (s *ServiceUser) LoginUser(ctx context.Context, req dto.Login, logId string, metadata dto.LoginMetadata) (string, error) {
	identifier, err := ResolveLoginIdentifier(req)
	if err != nil {
		return "", err
	}

	var (
		data     domainuser.Users
		loginErr error
	)
	if strings.Contains(identifier, "@") {
		data, loginErr = s.UserRepo.GetByEmail(ctx, identifier)
	} else {
		data, loginErr = s.UserRepo.GetByPhone(ctx, identifier)
	}
	if loginErr != nil {
		return "", loginErr
	}

	if err = bcrypt.CompareHashAndPassword([]byte(data.Password), []byte(req.Password)); err != nil {
		return "", err
	}

	data.LastLoginAt = new(time.Now())
	data.LastLoginIP = metadata.IP
	data.LastLoginUserAgent = metadata.UserAgent
	if data.LoginProvider == "" {
		data.LoginProvider = "local"
	}
	if err = s.UserRepo.Update(ctx, data); err != nil {
		return "", err
	}

	token, err := utils.GenerateJwt(&data, logId)
	if err != nil {
		return "", err
	}

	return token, nil
}

func (s *ServiceUser) LogoutUser(ctx context.Context, token string) error {
	expiresAt, err := utils.JwtExpiresAt(token)
	if err != nil {
		return err
	}

	blacklist := domainauth.Blacklist{
		ID:        utils.CreateUUID(),
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}

	err = s.BlacklistRepo.Store(ctx, blacklist)
	if err != nil {
		return err
	}

	return nil
}

func (s *ServiceUser) ImpersonateUser(ctx context.Context, targetUserId string, logId string) (string, error) {
	scope := authscope.FromContext(ctx)
	if scope.IsImpersonated {
		return "", errors.New("cannot start nested impersonation")
	}
	if strings.TrimSpace(targetUserId) == "" {
		return "", errors.New("target user id is required")
	}
	if targetUserId == scope.UserID {
		return "", errors.New("cannot impersonate your own account")
	}

	targetUser, err := s.UserRepo.GetByID(ctx, targetUserId)
	if err != nil {
		return "", err
	}

	if targetUser.Role == utils.RoleSuperAdmin && scope.Role != utils.RoleSuperAdmin {
		return "", errors.New("cannot impersonate superadmin users")
	}

	return utils.GenerateJwtWithClaims(&targetUser, logId, &utils.AppClaims{
		IsImpersonated:   true,
		OriginalUserId:   scope.UserID,
		OriginalUsername: scope.Username,
		OriginalRole:     scope.Role,
	})
}

func (s *ServiceUser) StopImpersonation(ctx context.Context, logId string) (string, error) {
	scope := authscope.FromContext(ctx)
	originalUserId := scope.OriginalUserID
	currentUserId := scope.UserID

	if strings.TrimSpace(originalUserId) == "" {
		return "", errors.New("original user id is required")
	}
	if originalUserId == currentUserId {
		return "", errors.New("current session is not impersonated")
	}

	originalUser, err := s.UserRepo.GetByID(ctx, originalUserId)
	if err != nil {
		return "", err
	}

	return utils.GenerateJwt(&originalUser, logId)
}

func (s *ServiceUser) GetUserById(ctx context.Context, id string) (domainuser.Users, error) {
	return s.UserRepo.GetByID(ctx, id)
}

func (s *ServiceUser) GetUserByEmail(ctx context.Context, email string) (domainuser.Users, error) {
	return s.UserRepo.GetByEmail(ctx, email)
}

func (s *ServiceUser) GetUserByPhone(ctx context.Context, phone string) (domainuser.Users, error) {
	return s.UserRepo.GetByPhone(ctx, phone)
}

func (s *ServiceUser) GetUserByAuth(ctx context.Context, id string) (map[string]any, error) {
	user, err := s.UserRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	permissions, err := s.PermissionRepo.GetUserPermissions(ctx, user.Id)
	if err != nil {
		//nolint:nilerr // Keep auth response available even if permission enrichment fails.
		return buildUserAuthResponse(user, nil), nil
	}

	permissionNames := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		permissionNames = append(permissionNames, perm.Name)
	}

	return buildUserAuthResponse(user, permissionNames), nil
}

func (s *ServiceUser) GetAllUsers(ctx context.Context, params filter.BaseParams) ([]domainuser.Users, int64, error) {
	scope := authscope.FromContext(ctx)
	users, total, err := s.UserRepo.GetAll(ctx, params)
	if err != nil {
		return nil, 0, err
	}

	if scope.Role != utils.RoleSuperAdmin {
		filteredUsers := make([]domainuser.Users, 0)
		for _, user := range users {
			if user.Role != utils.RoleSuperAdmin {
				filteredUsers = append(filteredUsers, user)
			}
		}
		superadminCount := int64(len(users) - len(filteredUsers))
		return filteredUsers, total - superadminCount, nil
	}

	return users, total, nil
}

func (s *ServiceUser) Update(ctx context.Context, id string, req dto.UserUpdate) (domainuser.Users, error) {
	scope := authscope.FromContext(ctx)
	data, err := s.UserRepo.GetByID(ctx, id)
	if err != nil {
		return domainuser.Users{}, err
	}

	if data.Role == utils.RoleSuperAdmin && scope.Role != utils.RoleSuperAdmin {
		return domainuser.Users{}, errors.New("cannot modify superadmin users")
	}

	if req.Name != "" {
		data.Name = utils.TitleCase(utils.StripHTML(req.Name))
	}

	if req.Phone != "" {
		phone := utils.NormalizePhoneTo62(req.Phone)
		data.Phone = phone
	}

	if req.Email != "" {
		data.Email = utils.SanitizeEmail(req.Email)
	}

	if reqRole := strings.TrimSpace(req.Role); reqRole != "" {
		newRoleName := utils.NormalizeKey(reqRole)
		canAssignRole, err := serviceshared.HasPermission(ctx, s.PermissionRepo, "users", "assign_role")
		if err != nil {
			return domainuser.Users{}, err
		}
		if !canAssignRole {
			return domainuser.Users{}, errors.New("access denied: missing permission users:assign_role")
		}
		if newRoleName == utils.RoleSuperAdmin && scope.Role != utils.RoleSuperAdmin {
			return domainuser.Users{}, errors.New("cannot assign superadmin role")
		}
		roleID, ok := findRoleIDByName(ctx, s.RoleRepo, newRoleName)
		if !ok {
			return domainuser.Users{}, errors.New("invalid role: " + newRoleName)
		}
		data.Role = newRoleName
		data.RoleId = roleID
	}

	if err = s.UserRepo.Update(ctx, data); err != nil {
		return domainuser.Users{}, err
	}
	if strings.TrimSpace(req.Role) != "" {
		s.invalidateUserPermissionCache(ctx, id)
	}

	return data, nil
}

func (s *ServiceUser) ChangePassword(ctx context.Context, id string, req dto.ChangePassword) (domainuser.Users, error) {
	if req.CurrentPassword == req.NewPassword {
		return domainuser.Users{}, errors.New("new password must be different from current password")
	}

	if err := ValidatePasswordStrength(req.NewPassword); err != nil {
		return domainuser.Users{}, err
	}

	data, err := s.UserRepo.GetByID(ctx, id)
	if err != nil {
		return domainuser.Users{}, err
	}

	if err = bcrypt.CompareHashAndPassword([]byte(data.Password), []byte(req.CurrentPassword)); err != nil {
		return domainuser.Users{}, err
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return domainuser.Users{}, err
	}

	data.Password = string(hashedPwd)
	data.PasswordChangedAt = new(time.Now())

	if err = s.UserRepo.Update(ctx, data); err != nil {
		return domainuser.Users{}, err
	}

	return data, nil
}

func (s *ServiceUser) ForgotPassword(ctx context.Context, req dto.ForgotPasswordRequest) (string, error) {
	data, err := s.UserRepo.GetByEmail(ctx, utils.SanitizeEmail(req.Email))
	if err != nil {
		//nolint:nilerr // Avoid disclosing whether an email is registered.
		return "", nil
	}

	token, err := utils.GeneratePasswordResetJwt(&data, "reset_password")
	if err != nil {
		return "", err
	}

	return token, nil
}

func (s *ServiceUser) ResetPassword(ctx context.Context, req dto.ResetPasswordRequest) error {
	if err := ValidatePasswordStrength(req.NewPassword); err != nil {
		return err
	}

	claims, err := utils.JwtClaim(req.Token)
	if err != nil {
		return errors.New("invalid or expired token")
	}
	if utils.InterfaceString(claims["token_type"]) != utils.TokenTypePasswordReset {
		return errors.New("invalid or expired token")
	}

	isUsed, err := s.BlacklistRepo.ExistsByToken(ctx, req.Token)
	if err != nil {
		return err
	}
	if isUsed {
		return errors.New("invalid or expired token")
	}

	userId := strings.TrimSpace(utils.InterfaceString(claims["user_id"]))
	if userId == "" {
		return errors.New("invalid or expired token")
	}

	data, err := s.UserRepo.GetByID(ctx, userId)
	if err != nil {
		return errors.New("user not found")
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	data.Password = string(hashedPwd)
	data.PasswordChangedAt = new(time.Now())

	if err = s.LogoutUser(ctx, req.Token); err != nil {
		return err
	}

	if err = s.UserRepo.Update(ctx, data); err != nil {
		return err
	}

	return nil
}

func (s *ServiceUser) ResetPasswordByEmail(ctx context.Context, email, newPassword string) error {
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}

	data, err := s.UserRepo.GetByEmail(ctx, utils.SanitizeEmail(email))
	if err != nil {
		return errors.New("user not found")
	}

	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	data.Password = string(hashedPwd)
	data.PasswordChangedAt = new(time.Now())

	if err = s.UserRepo.Update(ctx, data); err != nil {
		return err
	}

	return nil
}

func (s *ServiceUser) Delete(ctx context.Context, id string) error {
	if err := s.UserRepo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidateUserPermissionCache(ctx, id)
	return nil
}

func (s *ServiceUser) invalidateUserPermissionCache(ctx context.Context, userIDs ...string) {
	if s.PermissionCache != nil {
		s.PermissionCache.DeleteUser(ctx, userIDs...)
	}
}

var _ interfaceuser.ServiceUserInterface = (*ServiceUser)(nil)
