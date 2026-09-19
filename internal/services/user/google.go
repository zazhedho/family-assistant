package serviceuser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainuser "github.com/zazhedho/family-assistant/internal/domain/user"
	"github.com/zazhedho/family-assistant/internal/dto"
	"github.com/zazhedho/family-assistant/utils"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type googleTokenInfo struct {
	Audience      string `json:"aud"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Subject       string `json:"sub"`
}

var googleIDTokenVerifier = verifyGoogleIDToken

func (s *ServiceUser) LoginWithGoogle(ctx context.Context, req dto.GoogleLogin, metadata dto.LoginMetadata, allowRegistration bool) (domainuser.Users, bool, error) {
	identity, err := googleIDTokenVerifier(ctx, req.IDToken)
	if err != nil {
		return domainuser.Users{}, false, err
	}

	email := utils.SanitizeEmail(identity.Email)
	if email == "" {
		return domainuser.Users{}, false, ErrGoogleEmailMissing
	}

	existing, err := s.UserRepo.GetByEmail(ctx, email)
	if err == nil && existing.Id != "" {
		existing.EmailVerifiedAt = new(time.Now())
		existing.LastLoginAt = new(time.Now())
		existing.LastLoginIP = metadata.IP
		existing.LastLoginUserAgent = metadata.UserAgent
		existing.LoginProvider = "google"
		if strings.TrimSpace(identity.Picture) != "" {
			existing.AvatarURL = strings.TrimSpace(identity.Picture)
		}
		if updateErr := s.UserRepo.Update(ctx, existing); updateErr != nil {
			return domainuser.Users{}, false, updateErr
		}
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return domainuser.Users{}, false, err
	}
	if !allowRegistration {
		return domainuser.Users{}, false, ErrPublicRegistrationDisabled
	}

	roleName := utils.RoleViewer
	roleId, ok := findRoleIDByName(ctx, s.RoleRepo, roleName)
	if !ok {
		return domainuser.Users{}, false, errors.New("role viewer is not configured")
	}

	passwordSeed := "Google-" + utils.CreateUUID() + "-" + fmt.Sprintf("%d", time.Now().UnixNano()) + "!"
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(passwordSeed), bcrypt.DefaultCost)
	if err != nil {
		return domainuser.Users{}, false, err
	}

	name := strings.TrimSpace(identity.Name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	user := domainuser.Users{
		Id:                 utils.CreateUUID(),
		Name:               utils.TitleCase(name),
		Email:              email,
		Phone:              "",
		Password:           string(hashedPwd),
		Role:               roleName,
		RoleId:             roleId,
		EmailVerifiedAt:    new(time.Now()),
		LastLoginAt:        new(time.Now()),
		LastLoginIP:        metadata.IP,
		LastLoginUserAgent: metadata.UserAgent,
		PasswordChangedAt:  new(time.Now()),
		LoginProvider:      "google",
		AvatarURL:          strings.TrimSpace(identity.Picture),
		Metadata: map[string]any{
			"google_subject": identity.Subject,
		},
		CreatedAt: time.Now(),
	}

	if err := s.UserRepo.Store(ctx, user); err != nil {
		return domainuser.Users{}, false, err
	}

	return user, true, nil
}

func verifyGoogleIDToken(ctx context.Context, idToken string) (googleTokenInfo, error) {
	if strings.TrimSpace(idToken) == "" {
		return googleTokenInfo{}, ErrGoogleTokenInvalid
	}

	allowedAudiences := googleAllowedAudiences()
	if len(allowedAudiences) == 0 {
		return googleTokenInfo{}, ErrGoogleNotConfigured
	}

	endpoint := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(idToken)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return googleTokenInfo{}, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return googleTokenInfo{}, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return googleTokenInfo{}, ErrGoogleTokenInvalid
	}

	var tokenInfo googleTokenInfo
	if err := json.NewDecoder(response.Body).Decode(&tokenInfo); err != nil {
		return googleTokenInfo{}, err
	}

	if _, ok := allowedAudiences[strings.TrimSpace(tokenInfo.Audience)]; !ok {
		return googleTokenInfo{}, ErrGoogleTokenInvalid
	}
	if !strings.EqualFold(strings.TrimSpace(tokenInfo.EmailVerified), "true") {
		return googleTokenInfo{}, ErrGoogleTokenInvalid
	}
	if utils.SanitizeEmail(tokenInfo.Email) == "" {
		return googleTokenInfo{}, ErrGoogleEmailMissing
	}
	if strings.TrimSpace(tokenInfo.Subject) == "" {
		return googleTokenInfo{}, ErrGoogleTokenInvalid
	}

	return tokenInfo, nil
}

func googleAllowedAudiences() map[string]struct{} {
	values := make(map[string]struct{})

	rawList := utils.GetEnv("GOOGLE_CLIENT_IDS", "")
	if rawList != "" {
		for item := range strings.SplitSeq(rawList, ",") {
			normalized := strings.TrimSpace(item)
			if normalized != "" {
				values[normalized] = struct{}{}
			}
		}
	}

	single := utils.GetEnv("GOOGLE_CLIENT_ID", "")
	if single != "" {
		values[single] = struct{}{}
	}

	return values
}
