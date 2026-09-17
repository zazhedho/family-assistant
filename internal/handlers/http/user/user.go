package handleruser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"starter-kit/internal/authscope"
	domainaudit "starter-kit/internal/domain/audit"
	domainsession "starter-kit/internal/domain/session"
	domainuser "starter-kit/internal/domain/user"
	"starter-kit/internal/dto"
	handlercommon "starter-kit/internal/handlers/http/common"
	interfaceappconfig "starter-kit/internal/interfaces/appconfig"
	interfaceaudit "starter-kit/internal/interfaces/audit"
	interfaceauth "starter-kit/internal/interfaces/auth"
	interfaceotp "starter-kit/internal/interfaces/otp"
	interfacereset "starter-kit/internal/interfaces/reset"
	interfacesession "starter-kit/internal/interfaces/session"
	interfaceuser "starter-kit/internal/interfaces/user"
	serviceotp "starter-kit/internal/services/otp"
	servicereset "starter-kit/internal/services/reset"
	serviceuser "starter-kit/internal/services/user"
	"starter-kit/pkg/config"
	"starter-kit/pkg/filter"
	"starter-kit/pkg/logger"
	"starter-kit/pkg/messages"
	"starter-kit/pkg/response"
	"starter-kit/pkg/security"
	"starter-kit/utils"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type HandlerUser struct {
	Service       interfaceuser.ServiceUserInterface
	BlacklistRepo interfaceauth.RepoAuthInterface
	SessionSvc    interfacesession.ServiceSessionInterface
	LoginLimiter  security.LoginLimiter
	handlercommon.AuditWriter
	AppConfigService interfaceappconfig.ServiceAppConfigInterface
	OTPService       interfaceotp.ServiceOTPInterface
	ResetService     interfacereset.ServicePasswordResetInterface
}

func NewUserHandler(
	s interfaceuser.ServiceUserInterface,
	blacklistRepo interfaceauth.RepoAuthInterface,
	sessionSvc interfacesession.ServiceSessionInterface,
	limiter security.LoginLimiter,
	auditService interfaceaudit.ServiceAuditInterface,
	appConfigService interfaceappconfig.ServiceAppConfigInterface,
	otpService interfaceotp.ServiceOTPInterface,
	resetService interfacereset.ServicePasswordResetInterface,
) *HandlerUser {
	return &HandlerUser{
		Service:          s,
		BlacklistRepo:    blacklistRepo,
		SessionSvc:       sessionSvc,
		LoginLimiter:     limiter,
		AuditWriter:      handlercommon.NewAuditWriter(auditService, "UserHandler"),
		AppConfigService: appConfigService,
		OTPService:       otpService,
		ResetService:     resetService,
	}
}

func (h *HandlerUser) Register(ctx *gin.Context) {
	var req dto.UserRegister
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][Register]"
	reqCtx := ctx.Request.Context()

	registerEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_PUBLIC_REGISTRATION", defaultConfigPublicRegistrationEnabled), true)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if !registerEnabled {
		res := response.Forbidden(logId, "Public registration is currently disabled.")
		ctx.JSON(http.StatusForbidden, res)
		return
	}
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Request: %+v;", logPrefix, utils.JsonEncode(utils.RedactSensitivePayload(req))))

	otpEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_REGISTER_OTP", defaultConfigRegisterOTPEnabled), false)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if otpEnabled {
		if strings.TrimSpace(req.OTPCode) == "" {
			res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
			res.Error = response.Errors{Code: http.StatusBadRequest, Message: "otp_code is required"}
			ctx.JSON(http.StatusBadRequest, res)
			return
		}
		if h.OTPService == nil {
			res := response.Response(http.StatusServiceUnavailable, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{Code: http.StatusServiceUnavailable, Message: "registration OTP service is not configured"}
			ctx.JSON(http.StatusServiceUnavailable, res)
			return
		}
		if err := h.OTPService.VerifyRegisterOTP(ctx.Request.Context(), req.Email, req.OTPCode); err != nil {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				Action:       domainaudit.ActionCreate,
				Resource:     "user",
				Status:       domainaudit.StatusFailed,
				Message:      "Failed to verify registration OTP",
				ErrorMessage: err.Error(),
				AfterData: map[string]any{
					"email": req.Email,
				},
			})
			statusCode := http.StatusBadRequest
			message := "invalid or expired OTP"
			if errors.Is(err, serviceotp.ErrOTPTooManyAttempt) {
				message = "too many OTP attempts"
			}
			if errors.Is(err, serviceotp.ErrOTPNotConfigured) {
				statusCode = http.StatusServiceUnavailable
				message = "registration OTP service is not configured"
			}
			res := response.Response(statusCode, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{Code: statusCode, Message: message}
			ctx.JSON(statusCode, res)
			return
		}
		req.EmailVerified = true
	}

	data, err := h.Service.RegisterUser(reqCtx, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionCreate,
			Resource:     "user",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to register user",
			ErrorMessage: err.Error(),
			AfterData: map[string]any{
				"name":  req.Name,
				"email": req.Email,
				"phone": req.Phone,
			},
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.RegisterUser; Error: %+v", logPrefix, err))
		statusCode, res := userMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		ActorUserID: data.Id,
		ActorRole:   data.Role,
		Action:      domainaudit.ActionCreate,
		Resource:    "user",
		ResourceID:  data.Id,
		Status:      domainaudit.StatusSuccess,
		Message:     "Registered user",
		AfterData: map[string]any{
			"id":    data.Id,
			"name":  data.Name,
			"email": data.Email,
			"phone": data.Phone,
			"role":  data.Role,
		},
	})

	res := response.Response(http.StatusCreated, "User registered successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusCreated, res)
}

func (h *HandlerUser) GetRegisterStatus(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][GetRegisterStatus]"
	reqCtx := ctx.Request.Context()

	registerEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_PUBLIC_REGISTRATION", defaultConfigPublicRegistrationEnabled), true)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	otpEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_REGISTER_OTP", defaultConfigRegisterOTPEnabled), false)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "Get register status successfully", logId, map[string]any{
		"enabled":      registerEnabled,
		"otp_enabled":  otpEnabled,
		"otp_cooldown": utils.GetEnv("OTP_COOLDOWN_SECONDS", 60),
	})
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) SendRegisterOTP(ctx *gin.Context) {
	var req dto.SendRegisterOTPRequest
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][SendRegisterOTP]"
	reqCtx := ctx.Request.Context()

	registerEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_PUBLIC_REGISTRATION", defaultConfigPublicRegistrationEnabled), true)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if !registerEnabled {
		res := response.Forbidden(logId, "Public registration is currently disabled.")
		ctx.JSON(http.StatusForbidden, res)
		return
	}
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	otpEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_REGISTER_OTP", defaultConfigRegisterOTPEnabled), false)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if !otpEnabled {
		res := response.Response(http.StatusBadRequest, messages.MsgSomethingWrong, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "registration OTP is disabled"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}
	if h.OTPService == nil {
		res := response.Response(http.StatusServiceUnavailable, messages.MsgSomethingWrong, logId, nil)
		res.Error = response.Errors{Code: http.StatusServiceUnavailable, Message: "registration OTP service is not configured"}
		ctx.JSON(http.StatusServiceUnavailable, res)
		return
	}

	normalizedEmail := utils.SanitizeEmail(req.Email)
	data, err := h.Service.GetUserByEmail(reqCtx, normalizedEmail)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetUserByEmail; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if data.Id != "" {
		res := response.Response(http.StatusBadRequest, messages.MsgExists, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: "email already exists"}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	if req.Phone != "" {
		normalizedPhone := utils.NormalizePhoneTo62(req.Phone)
		data, err := h.Service.GetUserByPhone(reqCtx, normalizedPhone)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetUserByPhone; Error: %+v", logPrefix, err))
			res := response.InternalServerError(logId)
			ctx.JSON(http.StatusInternalServerError, res)
			return
		}
		if data.Id != "" {
			res := response.Response(http.StatusBadRequest, messages.MsgExists, logId, nil)
			res.Error = response.Errors{Code: http.StatusBadRequest, Message: "phone number already exists"}
			ctx.JSON(http.StatusBadRequest, res)
			return
		}
	}

	if err := h.OTPService.SendRegisterOTP(ctx.Request.Context(), normalizedEmail, utils.FirstNonEmptyString(utils.GetEnv("AUTH_EMAIL_APP_NAME", ""), utils.GetEnv("APP_NAME", "STARTER-KIT"))); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionCreate,
			Resource:     "user_registration_otp",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to send registration OTP",
			ErrorMessage: err.Error(),
			AfterData: map[string]any{
				"email": normalizedEmail,
			},
		})
		if throttle, ok := errors.AsType[*serviceotp.ThrottleError](err); ok {
			h.respondThrottle(ctx, logId, throttle.RetryAfter, "OTP request is throttled. Please try again later.")
			return
		}
		if errors.Is(err, serviceotp.ErrOTPNotConfigured) || errors.Is(err, serviceotp.ErrOTPDeliveryFailed) {
			statusCode := http.StatusServiceUnavailable
			res := response.ErrorResponse(statusCode, messages.MsgSomethingWrong, logId, "Registration OTP service is temporarily unavailable.")
			ctx.JSON(statusCode, res)
			return
		}

		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:   domainaudit.ActionCreate,
		Resource: "user_registration_otp",
		Status:   domainaudit.StatusSuccess,
		Message:  "Sent registration OTP",
		AfterData: map[string]any{
			"email": normalizedEmail,
		},
	})
	res := response.Response(http.StatusOK, "Registration OTP sent successfully", logId, nil)
	logger.WriteLogWithContext(ctx, logger.LogLevelInfo, fmt.Sprintf("%s; Registration OTP sent successfully", logPrefix))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) AdminCreateUser(ctx *gin.Context) {
	var req dto.AdminCreateUser
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][AdminCreateUser]"
	reqCtx := ctx.Request.Context()
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Request: %+v;", logPrefix, utils.JsonEncode(utils.RedactSensitivePayload(req))))

	data, err := h.Service.AdminCreateUser(reqCtx, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionCreate,
			Resource:     "user",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to create user",
			ErrorMessage: err.Error(),
			AfterData: map[string]any{
				"name":  req.Name,
				"email": req.Email,
				"phone": req.Phone,
				"role":  req.Role,
			},
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.AdminCreateUser; Error: %+v", logPrefix, err))
		statusCode, res := userMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionCreate,
		Resource:   "user",
		ResourceID: data.Id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Created user by admin",
		AfterData: map[string]any{
			"id":    data.Id,
			"name":  data.Name,
			"email": data.Email,
			"phone": data.Phone,
			"role":  data.Role,
		},
	})

	res := response.Response(http.StatusCreated, "User created successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusCreated, res)
}

func (h *HandlerUser) Login(ctx *gin.Context) {
	var req dto.Login
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserController][Login]"
	reqCtx := ctx.Request.Context()
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Request: %+v;", logPrefix, utils.JsonEncode(utils.RedactSensitivePayload(req))))

	normalizedIdentifier, err := serviceuser.ResolveLoginIdentifier(req)
	if err != nil {
		res := response.Response(http.StatusBadRequest, messages.InvalidRequest, logId, nil)
		res.Error = response.Errors{Code: http.StatusBadRequest, Message: err.Error()}
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	loginIdentifier := fmt.Sprintf("%s:%s", ctx.ClientIP(), normalizedIdentifier)
	if h.LoginLimiter != nil {
		blocked, ttl, limiterErr := h.LoginLimiter.IsBlocked(ctx.Request.Context(), loginIdentifier)
		if limiterErr != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; LoginLimiter.IsBlocked error: %v", logPrefix, limiterErr))
		} else if blocked {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				Action:   domainaudit.ActionLogin,
				Resource: "auth",
				Status:   domainaudit.StatusFailed,
				Message:  "Login blocked due to too many attempts",
				AfterData: map[string]any{
					"identifier": normalizedIdentifier,
				},
			})
			logger.WriteLogWithContext(ctx, logger.LogLevelWarn, fmt.Sprintf("%s; Too many attempts", logPrefix))
			h.respondTooManyLoginAttempts(ctx, logId, ttl)
			return
		}
	}

	token, err := h.Service.LoginUser(reqCtx, req, logId.String(), dto.LoginMetadata{
		IP:        ctx.ClientIP(),
		UserAgent: ctx.Request.UserAgent(),
	})
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.LoginUser; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) || err.Error() == messages.ErrHashPassword {
			if h.LoginLimiter != nil {
				blocked, ttl, limiterErr := h.LoginLimiter.RegisterFailure(ctx.Request.Context(), loginIdentifier)
				if limiterErr != nil {
					logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; LoginLimiter.RegisterFailure error: %v", logPrefix, limiterErr))
				}
				if blocked {
					h.WriteAudit(ctx, domainaudit.AuditEvent{
						Action:   domainaudit.ActionLogin,
						Resource: "auth",
						Status:   domainaudit.StatusFailed,
						Message:  "Login blocked after repeated failures",
						AfterData: map[string]any{
							"identifier": normalizedIdentifier,
						},
					})
					logger.WriteLogWithContext(ctx, logger.LogLevelWarn, fmt.Sprintf("%s; Account temporarily locked after repeated failures", logPrefix))
					h.respondTooManyLoginAttempts(ctx, logId, ttl)
					return
				}
			}

			h.WriteAudit(ctx, domainaudit.AuditEvent{
				Action:   domainaudit.ActionLogin,
				Resource: "auth",
				Status:   domainaudit.StatusFailed,
				Message:  "Login failed due to invalid credentials",
				AfterData: map[string]any{
					"identifier": normalizedIdentifier,
				},
			})
			res := response.Response(http.StatusBadRequest, messages.InvalidCred, logId, nil)
			res.Error = response.Errors{Code: http.StatusBadRequest, Message: messages.MsgCredential}
			ctx.JSON(http.StatusBadRequest, res)
			return
		}

		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionLogin,
			Resource:     "auth",
			Status:       domainaudit.StatusFailed,
			Message:      "Login failed due to internal error",
			ErrorMessage: err.Error(),
			AfterData: map[string]any{
				"identifier": normalizedIdentifier,
			},
		})
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	var (
		loggedInUser domainuser.Users
		userErr      error
	)
	if strings.Contains(normalizedIdentifier, "@") {
		loggedInUser, userErr = h.Service.GetUserByEmail(reqCtx, normalizedIdentifier)
	} else {
		loggedInUser, userErr = h.Service.GetUserByPhone(reqCtx, normalizedIdentifier)
	}
	loginUserID := ""
	if userErr == nil {
		loginUserID = loggedInUser.Id
	}

	refreshToken := ""
	if userErr == nil {
		refreshToken, err = utils.GenerateRefreshJwt(&loggedInUser, logId.String(), nil)
		if err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GenerateRefreshJwt; ERROR: %s;", logPrefix, err))
			res := response.InternalServerError(logId)
			ctx.JSON(http.StatusInternalServerError, res)
			return
		}
	}

	if h.LoginLimiter != nil {
		if err := h.LoginLimiter.Reset(ctx.Request.Context(), loginIdentifier); err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; LoginLimiter.Reset error: %v", logPrefix, err))
		}
	}

	// Create session if Redis is available
	if h.SessionSvc != nil && userErr == nil && refreshToken != "" {
		session, errSession := h.SessionSvc.CreateSession(reqCtx, &loggedInUser, token, refreshToken, domainsession.RequestMeta{
			IP:        ctx.ClientIP(),
			UserAgent: ctx.GetHeader("User-Agent"),
		})
		if errSession != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Failed to create session: %v", logPrefix, errSession))
		} else {
			logger.WriteLogWithContext(ctx, logger.LogLevelInfo, fmt.Sprintf("%s; Session created: %s", logPrefix, session.SessionID))
		}
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		ActorUserID: loginUserID,
		ActorRole:   loggedInUser.Role,
		Action:      domainaudit.ActionLogin,
		Resource:    "auth",
		ResourceID:  loginUserID,
		Status:      domainaudit.StatusSuccess,
		Message:     "Login success",
		AfterData: map[string]any{
			"identifier": normalizedIdentifier,
		},
	})

	data := buildAuthTokenResponse(token, refreshToken)
	res := response.Response(http.StatusOK, "success", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(utils.RedactSensitivePayload(data))))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) GoogleLogin(ctx *gin.Context) {
	var req dto.GoogleLogin
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][GoogleLogin]"
	reqCtx := ctx.Request.Context()
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	registerEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_PUBLIC_REGISTRATION", defaultConfigPublicRegistrationEnabled), true)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	user, isNewUser, err := h.Service.LoginWithGoogle(reqCtx, req, dto.LoginMetadata{
		IP:        ctx.ClientIP(),
		UserAgent: ctx.Request.UserAgent(),
	}, registerEnabled)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionLogin,
			Resource:     "auth",
			Status:       domainaudit.StatusFailed,
			Message:      "Google login failed",
			ErrorMessage: err.Error(),
			AfterData: map[string]any{
				"provider": "google",
			},
		})

		statusCode := http.StatusBadRequest
		switch {
		case errors.Is(err, serviceuser.ErrGoogleNotConfigured):
			statusCode = http.StatusServiceUnavailable
		case errors.Is(err, serviceuser.ErrGoogleTokenInvalid), errors.Is(err, serviceuser.ErrGoogleEmailMissing):
			statusCode = http.StatusUnauthorized
		case errors.Is(err, serviceuser.ErrPublicRegistrationDisabled):
			statusCode = http.StatusForbidden
		}

		res := response.InternalServerError(logId)
		switch {
		case errors.Is(err, serviceuser.ErrGoogleNotConfigured):
			res = response.ErrorResponse(statusCode, messages.MsgSomethingWrong, logId, "Google login is not configured.")
		case errors.Is(err, serviceuser.ErrGoogleTokenInvalid):
			res = response.ErrorResponse(statusCode, messages.MsgSomethingWrong, logId, "Invalid Google token.")
		case errors.Is(err, serviceuser.ErrGoogleEmailMissing):
			res = response.ErrorResponse(statusCode, messages.MsgSomethingWrong, logId, "Google account email is not available.")
		case errors.Is(err, serviceuser.ErrPublicRegistrationDisabled):
			res = response.Forbidden(logId, "Public registration is currently disabled.")
		}
		ctx.JSON(statusCode, res)
		return
	}

	accessToken, err := utils.GenerateJwt(&user, logId.String())
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GenerateJwt; ERROR: %s;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	refreshToken, err := utils.GenerateRefreshJwt(&user, logId.String(), nil)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GenerateRefreshJwt; ERROR: %s;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	if h.SessionSvc != nil {
		session, errSession := h.SessionSvc.CreateSession(reqCtx, &user, accessToken, refreshToken, domainsession.RequestMeta{
			IP:        ctx.ClientIP(),
			UserAgent: ctx.GetHeader("User-Agent"),
		})
		if errSession != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Failed to create session: %v", logPrefix, errSession))
		} else {
			logger.WriteLogWithContext(ctx, logger.LogLevelInfo, fmt.Sprintf("%s; Session created: %s", logPrefix, session.SessionID))
		}
	}

	successMessage := "Google login success"
	if isNewUser {
		successMessage = "Google registration success"
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		ActorUserID: user.Id,
		ActorRole:   user.Role,
		Action:      domainaudit.ActionLogin,
		Resource:    "auth",
		ResourceID:  user.Id,
		Status:      domainaudit.StatusSuccess,
		Message:     successMessage,
		AfterData: map[string]any{
			"provider":    "google",
			"email":       user.Email,
			"is_new_user": isNewUser,
		},
	})

	data := buildAuthTokenResponse(accessToken, refreshToken)
	data["is_new_user"] = isNewUser
	data["provider"] = "google"

	res := response.Response(http.StatusOK, successMessage, logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelInfo, fmt.Sprintf("%s; %s; Data: %+v", logPrefix, successMessage, utils.JsonEncode(utils.RedactSensitivePayload(data))))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) RefreshToken(ctx *gin.Context) {
	var req dto.RefreshTokenRequest
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][RefreshToken]"
	reqCtx := ctx.Request.Context()
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	tokenClaims, err := utils.JwtClaim(req.RefreshToken)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to renew login session",
			ErrorMessage: "The refresh token is invalid or expired",
		})
		res := response.Response(http.StatusUnauthorized, messages.MsgSomethingWrong, logId, nil)
		res.Error = "invalid or expired refresh token"
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	if !strings.EqualFold(utils.InterfaceString(tokenClaims["token_type"]), "refresh") {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			ActorUserID:  utils.InterfaceString(tokenClaims["user_id"]),
			ActorRole:    utils.InterfaceString(tokenClaims["role"]),
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to renew login session",
			ErrorMessage: "The provided token is not a refresh token",
		})
		res := response.Response(http.StatusUnauthorized, messages.MsgSomethingWrong, logId, nil)
		res.Error = "invalid token type"
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	isBlacklisted, err := h.BlacklistRepo.ExistsByToken(ctx.Request.Context(), req.RefreshToken)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			ActorUserID:  utils.InterfaceString(tokenClaims["user_id"]),
			ActorRole:    utils.InterfaceString(tokenClaims["role"]),
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to validate login session renewal",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; BlacklistRepo.ExistsByToken; ERROR: %s;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if isBlacklisted {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			ActorUserID:  utils.InterfaceString(tokenClaims["user_id"]),
			ActorRole:    utils.InterfaceString(tokenClaims["role"]),
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to renew login session",
			ErrorMessage: "The refresh token has already been revoked",
		})
		res := response.Response(http.StatusUnauthorized, messages.MsgSomethingWrong, logId, nil)
		res.Error = "refresh token has been revoked"
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	userID := utils.InterfaceString(tokenClaims["user_id"])
	userRole := utils.InterfaceString(tokenClaims["role"])
	user, err := h.Service.GetUserById(reqCtx, userID)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			ActorUserID:  userID,
			ActorRole:    userRole,
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			ResourceID:   userID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to renew login session",
			ErrorMessage: "The token owner user account was not found",
		})
		statusCode := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			statusCode = http.StatusNotFound
		}
		res := response.Response(statusCode, messages.MsgSomethingWrong, logId, nil)
		res.Error = "user not found"
		ctx.JSON(statusCode, res)
		return
	}

	claimsOverride := buildImpersonationClaimsOverrideFromClaims(tokenClaims)
	accessToken, err := utils.GenerateJwtWithClaims(&user, logId.String(), claimsOverride)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			ActorUserID:  userID,
			ActorRole:    user.Role,
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			ResourceID:   userID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to issue a new access token",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GenerateJwtWithClaims; ERROR: %s;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	refreshToken, err := utils.GenerateRefreshJwt(&user, logId.String(), claimsOverride)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			ActorUserID:  userID,
			ActorRole:    user.Role,
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			ResourceID:   userID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to issue a new refresh token",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GenerateRefreshJwt; ERROR: %s;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	if h.SessionSvc != nil {
		session, sessionErr := h.SessionSvc.GetSessionByRefreshToken(reqCtx, req.RefreshToken)
		if sessionErr != nil {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				ActorUserID:  userID,
				ActorRole:    user.Role,
				Action:       domainaudit.ActionRefresh,
				Resource:     "auth_token",
				ResourceID:   userID,
				Status:       domainaudit.StatusFailed,
				Message:      "Failed to renew login session",
				ErrorMessage: "No active session was found for the refresh token",
			})
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; SessionSvc.GetSessionByRefreshToken; ERROR: %s;", logPrefix, sessionErr))
			res := response.Response(http.StatusUnauthorized, messages.MsgSomethingWrong, logId, nil)
			res.Error = "session not found for refresh token"
			ctx.JSON(http.StatusUnauthorized, res)
			return
		}

		refreshExpAt := time.Now().Add(time.Hour * time.Duration(utils.GetEnv("REFRESH_TOKEN_EXP_HOURS", 168)))
		if sessionErr = h.SessionSvc.RotateSessionTokens(reqCtx, session.SessionID, accessToken, refreshToken, refreshExpAt); sessionErr != nil {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				ActorUserID:  userID,
				ActorRole:    user.Role,
				Action:       domainaudit.ActionRefresh,
				Resource:     "auth_token",
				ResourceID:   userID,
				Status:       domainaudit.StatusFailed,
				Message:      "Failed to rotate login session tokens",
				ErrorMessage: sessionErr.Error(),
				Metadata: map[string]any{
					"session_id": session.SessionID,
				},
			})
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; SessionSvc.RotateSessionTokens; ERROR: %s;", logPrefix, sessionErr))
			res := response.InternalServerError(logId)
			ctx.JSON(http.StatusInternalServerError, res)
			return
		}
	}

	if err = h.Service.LogoutUser(reqCtx, req.RefreshToken); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			ActorUserID:  userID,
			ActorRole:    user.Role,
			Action:       domainaudit.ActionRefresh,
			Resource:     "auth_token",
			ResourceID:   userID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to revoke previous login session token",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.LogoutUser(refresh); ERROR: %s;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		ActorUserID: userID,
		ActorRole:   user.Role,
		Action:      domainaudit.ActionRefresh,
		Resource:    "auth_token",
		ResourceID:  userID,
		Status:      domainaudit.StatusSuccess,
		Message:     "Renewed login session",
	})

	res := response.Response(http.StatusOK, "Refresh token rotated successfully", logId, buildAuthTokenResponse(accessToken, refreshToken))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) Logout(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserController][Logout]"
	reqCtx := ctx.Request.Context()

	token, ok := ctx.Get("token")
	if !ok {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionLogout,
			Resource:     "auth",
			Status:       domainaudit.StatusFailed,
			Message:      "Logout failed because token missing in context",
			ErrorMessage: "token not found",
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; token not found in context", logPrefix))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	if h.SessionSvc != nil {
		errSession := h.SessionSvc.DestroySessionByToken(reqCtx, token.(string))
		if errSession != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Failed to destroy session: %v", logPrefix, errSession))
		} else {
			logger.WriteLogWithContext(ctx, logger.LogLevelInfo, fmt.Sprintf("%s; Session destroyed successfully", logPrefix))
		}
	}

	if err := h.Service.LogoutUser(reqCtx, token.(string)); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionLogout,
			Resource:     "auth",
			Status:       domainaudit.StatusFailed,
			Message:      "Logout failed",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.LogoutUser; Error: %+v", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:   domainaudit.ActionLogout,
		Resource: "auth",
		Status:   domainaudit.StatusSuccess,
		Message:  "Logout success",
	})

	res := response.Response(http.StatusOK, "User logged out successfully", logId, nil)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Success: User logged out successfully", logPrefix))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) GetUserById(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][GetUserByID]"
	reqCtx := ctx.Request.Context()

	id, err := utils.ValidateUUID(ctx, logId)
	if err != nil {
		return
	}

	data, err := h.Service.GetUserById(reqCtx, id)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetUserByID; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "user not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.Response(http.StatusOK, "success", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) GetUserByAuth(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][GetUserByAuth]"
	reqCtx := ctx.Request.Context()
	scope := authscope.FromContext(reqCtx)

	if scope.UserID == "" {
		res := response.Response(http.StatusUnauthorized, "Unauthorized", logId, nil)
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	data, err := h.Service.GetUserByAuth(reqCtx, scope.UserID)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.GetUserByAuth; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "user not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	if scope.IsImpersonated {
		data["is_impersonated"] = true
		data["impersonator"] = map[string]any{
			"user_id":  scope.OriginalUserID,
			"username": scope.OriginalUsername,
			"role":     scope.OriginalRole,
		}
	} else {
		data["is_impersonated"] = false
	}

	res := response.Response(http.StatusOK, "success", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) ImpersonateUser(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][ImpersonateUser]"
	reqCtx := ctx.Request.Context()

	id, err := utils.ValidateUUID(ctx, logId)
	if err != nil {
		return
	}

	token, err := h.Service.ImpersonateUser(reqCtx, id, logId.String())
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionLogin,
			Resource:     "user_impersonation",
			ResourceID:   id,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to impersonate user",
			ErrorMessage: err.Error(),
			AfterData: map[string]any{
				"target_user_id": id,
			},
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.ImpersonateUser; ERROR: %s;", logPrefix, err))

		statusCode, res := impersonationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionLogin,
		Resource:   "user_impersonation",
		ResourceID: id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Started impersonation session",
		AfterData: map[string]any{
			"target_user_id": id,
		},
		Metadata: map[string]any{
			"impersonation_action": "start",
			"target_user_id":       id,
		},
	})

	res := response.Response(http.StatusOK, "Impersonation started successfully", logId, buildAuthTokenResponse(token, ""))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) StopImpersonation(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][StopImpersonation]"
	reqCtx := ctx.Request.Context()

	scope := authscope.FromContext(reqCtx)
	originalUserID := scope.OriginalUserID
	if !scope.IsImpersonated || originalUserID == "" {
		res := response.Response(http.StatusBadRequest, messages.MsgSomethingWrong, logId, nil)
		res.Error = "current session is not impersonated"
		ctx.JSON(http.StatusBadRequest, res)
		return
	}

	token, err := h.Service.StopImpersonation(reqCtx, logId.String())
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionLogout,
			Resource:     "user_impersonation",
			ResourceID:   originalUserID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to stop impersonation",
			ErrorMessage: err.Error(),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.StopImpersonation; ERROR: %s;", logPrefix, err))

		statusCode, res := impersonationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}

	if tokenString, ok := ctx.Get("token"); ok {
		_ = h.Service.LogoutUser(reqCtx, tokenString.(string))
	}

	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionLogout,
		Resource:   "user_impersonation",
		ResourceID: originalUserID,
		Status:     domainaudit.StatusSuccess,
		Message:    "Stopped impersonation session",
		Metadata: map[string]any{
			"impersonation_action": "stop",
			"restored_user_id":     originalUserID,
		},
	})

	res := response.Response(http.StatusOK, "Impersonation stopped successfully", logId, buildAuthTokenResponse(token, ""))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) GetAllUsers(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][GetAllUsers]"
	reqCtx := ctx.Request.Context()

	params, _ := filter.GetBaseParams(ctx, "updated_at", "desc", 10)
	params.Filters = filter.WhitelistFilter(params.Filters, []string{"role"})

	users, totalData, err := h.Service.GetAllUsers(reqCtx, params)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; GetAllUsers; ERROR: %+v;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}

	res := response.PaginationResponse(http.StatusOK, int(totalData), params.Page, params.Limit, logId, users)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(users)))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) Update(ctx *gin.Context) {
	var req dto.UserUpdate
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][Update]"
	reqCtx := ctx.Request.Context()
	scope := authscope.FromContext(reqCtx)

	if scope.UserID == "" {
		res := response.Response(http.StatusUnauthorized, "Unauthorized", logId, nil)
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	before, _ := h.Service.GetUserById(reqCtx, scope.UserID)
	data, err := h.Service.Update(reqCtx, scope.UserID, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionUpdate,
			Resource:     "user",
			ResourceID:   scope.UserID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to update user profile",
			ErrorMessage: err.Error(),
			BeforeData:   before,
			AfterData:    req,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Update; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "user not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		statusCode, res := userMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionUpdate,
		Resource:   "user",
		ResourceID: data.Id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Updated user profile",
		BeforeData: before,
		AfterData:  data,
	})

	res := response.Response(http.StatusOK, "User updated successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) UpdateUserById(ctx *gin.Context) {
	var req dto.UserUpdate
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][UpdateUserById]"
	reqCtx := ctx.Request.Context()

	id, err := utils.ValidateUUID(ctx, logId)
	if err != nil {
		return
	}
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Request: %+v;", logPrefix, utils.JsonEncode(req)))

	before, _ := h.Service.GetUserById(reqCtx, id)
	data, err := h.Service.Update(reqCtx, id, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionUpdate,
			Resource:     "user",
			ResourceID:   id,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to update user by ID",
			ErrorMessage: err.Error(),
			BeforeData:   before,
			AfterData:    req,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Update; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "user not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		statusCode, res := userMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionUpdate,
		Resource:   "user",
		ResourceID: data.Id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Updated user by ID",
		BeforeData: before,
		AfterData:  data,
	})

	res := response.Response(http.StatusOK, "User updated successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) ChangePassword(ctx *gin.Context) {
	var req dto.ChangePassword
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][ChangePassword]"
	reqCtx := ctx.Request.Context()
	scope := authscope.FromContext(reqCtx)

	if scope.UserID == "" {
		res := response.Response(http.StatusUnauthorized, "Unauthorized", logId, nil)
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	before, _ := h.Service.GetUserById(reqCtx, scope.UserID)
	data, err := h.Service.ChangePassword(reqCtx, scope.UserID, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionUpdate,
			Resource:     "user_password",
			ResourceID:   scope.UserID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to change password",
			ErrorMessage: err.Error(),
			BeforeData:   before,
			AfterData:    req,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.ChangePassword; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "user not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		if err.Error() == messages.ErrHashPassword {
			res := response.Response(http.StatusBadRequest, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{Code: http.StatusBadRequest, Message: "current password is incorrect"}
			ctx.JSON(http.StatusBadRequest, res)
			return
		}

		statusCode, res := userMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionUpdate,
		Resource:   "user_password",
		ResourceID: data.Id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Changed user password",
		BeforeData: before,
		AfterData: map[string]any{
			"user_id": data.Id,
		},
	})

	res := response.Response(http.StatusOK, "User password changed successfully", logId, data)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Response: %+v;", logPrefix, utils.JsonEncode(data)))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) ForgotPassword(ctx *gin.Context) {
	var req dto.ForgotPasswordRequest
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][ForgotPassword]"
	reqCtx := ctx.Request.Context()
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	emailResetEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_PASSWORD_RESET_EMAIL", defaultConfigPasswordResetEmailEnabled), false)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if emailResetEnabled {
		if h.ResetService == nil {
			res := response.Response(http.StatusServiceUnavailable, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{Code: http.StatusServiceUnavailable, Message: "password reset email service is not configured"}
			ctx.JSON(http.StatusServiceUnavailable, res)
			return
		}

		normalizedEmail := utils.SanitizeEmail(req.Email)
		if data, err := h.Service.GetUserByEmail(reqCtx, normalizedEmail); err == nil && data.Id != "" {
			if err := h.ResetService.RequestReset(ctx.Request.Context(), normalizedEmail, utils.FirstNonEmptyString(utils.GetEnv("AUTH_EMAIL_APP_NAME", ""), utils.GetEnv("APP_NAME", "STARTER-KIT"))); err != nil {
				h.WriteAudit(ctx, domainaudit.AuditEvent{
					Action:       domainaudit.ActionUpdate,
					Resource:     "user_password_reset",
					Status:       domainaudit.StatusFailed,
					Message:      "Failed to request password reset email",
					ErrorMessage: err.Error(),
					AfterData: map[string]any{
						"email": normalizedEmail,
					},
				})
				if throttle, ok := errors.AsType[*servicereset.ThrottleError](err); ok {
					h.respondThrottle(ctx, logId, throttle.RetryAfter, "Password reset request is throttled. Please try again later.")
					return
				}
				statusCode := http.StatusInternalServerError
				message := "Failed to send password reset email. Please contact support with the log ID."
				if errors.Is(err, servicereset.ErrResetNotConfigured) || errors.Is(err, servicereset.ErrResetDeliveryFailed) {
					statusCode = http.StatusServiceUnavailable
					message = "Password reset email service is temporarily unavailable."
				}
				res := response.Response(statusCode, messages.MsgSomethingWrong, logId, nil)
				res.Error = response.Errors{Code: statusCode, Message: message}
				ctx.JSON(statusCode, res)
				return
			}
		}

		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:   domainaudit.ActionUpdate,
			Resource: "user_password_reset",
			Status:   domainaudit.StatusSuccess,
			Message:  "Requested password reset email",
			AfterData: map[string]any{
				"email": normalizedEmail,
			},
		})
		res := response.Response(http.StatusOK, "Password reset instructions sent to your email", logId, map[string]any{
			"cooldown": int(config.LoadPasswordResetConfig().Cooldown.Seconds()),
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelInfo, fmt.Sprintf("%s; Password reset instructions sent to email: %s", logPrefix, normalizedEmail))
		ctx.JSON(http.StatusOK, res)
		return
	}

	appEnv := utils.NormalizeKey(utils.GetEnv("APP_ENV", ""))
	if appEnv != "development" && appEnv != "local" && appEnv != "test" {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Development password reset fallback is disabled in APP_ENV=%s", logPrefix, appEnv))
		res := response.Response(http.StatusServiceUnavailable, messages.MsgSomethingWrong, logId, nil)
		res.Error = response.Errors{Code: http.StatusServiceUnavailable, Message: "Password reset service is temporarily unavailable."}
		ctx.JSON(http.StatusServiceUnavailable, res)
		return
	}

	token, err := h.Service.ForgotPassword(reqCtx, req)
	if err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionUpdate,
			Resource:     "user_password_reset",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to request password reset",
			ErrorMessage: err.Error(),
			AfterData: map[string]any{
				"email": req.Email,
			},
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.ForgotPassword; ERROR: %s;", logPrefix, err))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:   domainaudit.ActionUpdate,
		Resource: "user_password_reset",
		Status:   domainaudit.StatusSuccess,
		Message:  "Requested password reset",
		AfterData: map[string]any{
			"email": req.Email,
		},
	})

	logger.WriteLogWithContext(ctx, logger.LogLevelInfo, fmt.Sprintf("%s; Development password reset token generated for %s", logPrefix, utils.SanitizeEmail(req.Email)))

	res := response.Response(http.StatusOK, "Password reset instructions sent to your email", logId, token)
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) ResetPassword(ctx *gin.Context) {
	var req dto.ResetPasswordRequest
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][ResetPassword]"
	reqCtx := ctx.Request.Context()
	if !handlercommon.BindJSON(ctx, logId, logPrefix, &req) {
		return
	}

	emailResetEnabled, err := h.isRuntimeConfigEnabled(reqCtx, utils.GetEnv("CONFIG_PASSWORD_RESET_EMAIL", defaultConfigPasswordResetEmailEnabled), false)
	if err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Config check ERROR: %s;", logPrefix, err.Error()))
		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	if emailResetEnabled {
		if h.ResetService == nil {
			res := response.Response(http.StatusServiceUnavailable, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{Code: http.StatusServiceUnavailable, Message: "password reset email service is not configured"}
			ctx.JSON(http.StatusServiceUnavailable, res)
			return
		}

		email, err := h.ResetService.VerifyReset(ctx.Request.Context(), req.Token)
		if err != nil {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				Action:       domainaudit.ActionUpdate,
				Resource:     "user_password_reset",
				Status:       domainaudit.StatusFailed,
				Message:      "Failed to verify password reset token",
				ErrorMessage: err.Error(),
			})
			statusCode := http.StatusBadRequest
			message := "invalid or expired reset token"
			if errors.Is(err, servicereset.ErrResetNotConfigured) {
				statusCode = http.StatusServiceUnavailable
				message = "password reset email service is not configured"
			}
			res := response.Response(statusCode, messages.MsgSomethingWrong, logId, nil)
			res.Error = response.Errors{Code: statusCode, Message: message}
			ctx.JSON(statusCode, res)
			return
		}
		userID := ""
		if h.SessionSvc != nil {
			user, err := h.Service.GetUserByEmail(reqCtx, email)
			if err != nil {
				h.WriteAudit(ctx, domainaudit.AuditEvent{
					Action:       domainaudit.ActionUpdate,
					Resource:     "user_password_reset",
					Status:       domainaudit.StatusFailed,
					Message:      "Failed to load user for password reset",
					ErrorMessage: err.Error(),
					AfterData: map[string]any{
						"email": email,
					},
				})
				statusCode, res := userMutationErrorResponse(logId, err)
				ctx.JSON(statusCode, res)
				return
			}
			userID = user.Id
		}
		if err := h.Service.ResetPasswordByEmail(reqCtx, email, req.NewPassword); err != nil {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				Action:       domainaudit.ActionUpdate,
				Resource:     "user_password_reset",
				Status:       domainaudit.StatusFailed,
				Message:      "Failed to reset password",
				ErrorMessage: err.Error(),
				AfterData: map[string]any{
					"email": email,
				},
			})
			statusCode, res := userMutationErrorResponse(logId, err)
			ctx.JSON(statusCode, res)
			return
		}
		if err := h.revokePasswordResetSessions(reqCtx, userID); err != nil {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				Action:       domainaudit.ActionUpdate,
				Resource:     "user_password_reset",
				Status:       domainaudit.StatusFailed,
				Message:      "Failed to revoke sessions after password reset",
				ErrorMessage: err.Error(),
				AfterData: map[string]any{
					"email": email,
				},
			})
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; SessionSvc.DestroyAllUserSessions; ERROR: %s;", logPrefix, err))
			res := response.InternalServerError(logId)
			ctx.JSON(http.StatusInternalServerError, res)
			return
		}

		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:   domainaudit.ActionUpdate,
			Resource: "user_password_reset",
			Status:   domainaudit.StatusSuccess,
			Message:  "Reset password success",
			AfterData: map[string]any{
				"email": email,
			},
		})
		res := response.Response(http.StatusOK, "Password reset successfully", logId, nil)
		ctx.JSON(http.StatusOK, res)
		return
	}

	if err := h.Service.ResetPassword(reqCtx, req); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionUpdate,
			Resource:     "user_password_reset",
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to reset password",
			ErrorMessage: err.Error(),
			AfterData:    req,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.ResetPassword; ERROR: %s;", logPrefix, err))
		statusCode, res := userMutationErrorResponse(logId, err)
		ctx.JSON(statusCode, res)
		return
	}
	if h.SessionSvc != nil {
		claims, err := utils.JwtClaim(req.Token)
		if err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; JwtClaim after reset; ERROR: %s;", logPrefix, err))
			res := response.InternalServerError(logId)
			ctx.JSON(http.StatusInternalServerError, res)
			return
		}
		if err := h.revokePasswordResetSessions(reqCtx, utils.InterfaceString(claims["user_id"])); err != nil {
			h.WriteAudit(ctx, domainaudit.AuditEvent{
				Action:       domainaudit.ActionUpdate,
				Resource:     "user_password_reset",
				Status:       domainaudit.StatusFailed,
				Message:      "Failed to revoke sessions after password reset",
				ErrorMessage: err.Error(),
				AfterData:    req,
			})
			logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; SessionSvc.DestroyAllUserSessions; ERROR: %s;", logPrefix, err))
			res := response.InternalServerError(logId)
			ctx.JSON(http.StatusInternalServerError, res)
			return
		}
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:   domainaudit.ActionUpdate,
		Resource: "user_password_reset",
		Status:   domainaudit.StatusSuccess,
		Message:  "Reset password success",
		AfterData: map[string]any{
			"token": req.Token,
		},
	})

	res := response.Response(http.StatusOK, "Password reset successfully", logId, nil)
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) revokePasswordResetSessions(ctx context.Context, userID string) error {
	if h.SessionSvc == nil || strings.TrimSpace(userID) == "" {
		return nil
	}

	return h.SessionSvc.DestroyAllUserSessions(ctx, userID)
}

func (h *HandlerUser) Delete(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][Delete]"
	reqCtx := ctx.Request.Context()
	scope := authscope.FromContext(reqCtx)

	if scope.UserID == "" {
		res := response.Response(http.StatusUnauthorized, "Unauthorized", logId, nil)
		ctx.JSON(http.StatusUnauthorized, res)
		return
	}

	before, _ := h.Service.GetUserById(reqCtx, scope.UserID)

	if err := h.Service.Delete(reqCtx, scope.UserID); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "user",
			ResourceID:   scope.UserID,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to delete own user",
			ErrorMessage: err.Error(),
			BeforeData:   before,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Delete; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "user not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionDelete,
		Resource:   "user",
		ResourceID: scope.UserID,
		Status:     domainaudit.StatusSuccess,
		Message:    "Deleted own user",
		BeforeData: before,
	})

	res := response.Response(http.StatusOK, "User deleted successfully", logId, nil)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Success: User deleted successfully", logPrefix))
	ctx.JSON(http.StatusOK, res)
}

func (h *HandlerUser) DeleteUserById(ctx *gin.Context) {
	logId := utils.GenerateLogId(ctx)
	logPrefix := "[UserHandler][DeleteUserById]"
	reqCtx := ctx.Request.Context()

	id, err := utils.ValidateUUID(ctx, logId)
	if err != nil {
		return
	}
	before, _ := h.Service.GetUserById(reqCtx, id)

	if err := h.Service.Delete(reqCtx, id); err != nil {
		h.WriteAudit(ctx, domainaudit.AuditEvent{
			Action:       domainaudit.ActionDelete,
			Resource:     "user",
			ResourceID:   id,
			Status:       domainaudit.StatusFailed,
			Message:      "Failed to delete user by ID",
			ErrorMessage: err.Error(),
			BeforeData:   before,
		})
		logger.WriteLogWithContext(ctx, logger.LogLevelError, fmt.Sprintf("%s; Service.Delete; ERROR: %s;", logPrefix, err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			res := response.Response(http.StatusNotFound, messages.MsgNotFound, logId, nil)
			res.Error = response.Errors{Code: http.StatusNotFound, Message: "user not found"}
			ctx.JSON(http.StatusNotFound, res)
			return
		}

		res := response.InternalServerError(logId)
		ctx.JSON(http.StatusInternalServerError, res)
		return
	}
	h.WriteAudit(ctx, domainaudit.AuditEvent{
		Action:     domainaudit.ActionDelete,
		Resource:   "user",
		ResourceID: id,
		Status:     domainaudit.StatusSuccess,
		Message:    "Deleted user by ID",
		BeforeData: before,
	})

	res := response.Response(http.StatusOK, "User deleted successfully", logId, nil)
	logger.WriteLogWithContext(ctx, logger.LogLevelDebug, fmt.Sprintf("%s; Success: User deleted successfully", logPrefix))
	ctx.JSON(http.StatusOK, res)
}
