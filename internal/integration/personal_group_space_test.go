package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainspace "family-assistant/internal/domain/space"
	mcpHandler "family-assistant/internal/handlers/mcp"
	auditRepo "family-assistant/internal/repositories/audit"
	identityRepo "family-assistant/internal/repositories/identity"
	permissionRepo "family-assistant/internal/repositories/permission"
	reminderRepo "family-assistant/internal/repositories/reminder"
	roleRepo "family-assistant/internal/repositories/role"
	spaceRepo "family-assistant/internal/repositories/space"
	"family-assistant/internal/router"
	auditService "family-assistant/internal/services/audit"
	"family-assistant/internal/services/authorization"
	identityService "family-assistant/internal/services/identity"
	permissionService "family-assistant/internal/services/permission"
	reminderService "family-assistant/internal/services/reminder"
	spaceService "family-assistant/internal/services/space"
	"family-assistant/pkg/config"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPersonalGroupSpaceArchitecture(t *testing.T) {
	rawDSN, dsnSet := os.LookupEnv("FAMILY_ASSISTANT_TEST_DATABASE_URL")
	dsn := strings.TrimSpace(rawDSN)
	if !dsnSet {
		t.Skip("FAMILY_ASSISTANT_TEST_DATABASE_URL is not set")
	}
	if dsn == "" {
		t.Fatal("FAMILY_ASSISTANT_TEST_DATABASE_URL must not be empty when set")
	}
	assertSafeTestDSN(t, dsn)
	if err := loadIntegrationEnv("../../.env"); err != nil {
		t.Fatalf("worktree .env is required when FAMILY_ASSISTANT_TEST_DATABASE_URL is set: %v", err)
	}
	t.Setenv("CONFIG_PUBLIC_REGISTRATION", "auth.public_registration_enabled")
	t.Setenv("CONFIG_REGISTER_OTP", "auth.register_otp_enabled")
	t.Setenv("MIN_INDEPENDENT_ACCOUNT_AGE", "18")
	t.Setenv("SPACE_INVITATION_TTL_SECONDS", "3600")
	t.Setenv("IDENTITY_LINK_TTL_SECONDS", "600")

	db := resetIntegrationDatabase(t, dsn)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	audits := auditService.NewAuditService(auditRepo.NewAuditRepo(db))
	permissionsRepo := permissionRepo.NewPermissionRepo(db)
	permissions := permissionService.NewPermissionService(permissionsRepo)
	spacesRepo := spaceRepo.NewRepository(db)
	rolesRepo := roleRepo.NewRoleRepo(db)
	spaces := spaceService.NewService(spacesRepo, rolesRepo, permissionsRepo, audits)
	identities := identityService.NewResolver(identityRepo.NewRepository(db), spacesRepo, permissions)
	links := identityService.NewLinkService(identityRepo.NewRepository(db), audits, config.IdentityConfig{TTL: 10 * time.Minute})
	reminders := reminderService.NewReminderService(
		reminderRepo.NewRepository(db), spacesRepo, authorization.NewAuthorizer(), audits,
	)

	routes := router.NewRoutes()
	routes.DB = db
	routes.UserRoutes()
	routes.RoleRoutes()
	routes.PermissionRoutes()
	routes.MenuRoutes()
	routes.AppConfigRoutes()
	routes.AuditRoutes()
	routes.LocationRoutes()
	routes.SpaceRoutes()
	routes.ReminderRoutes(reminders, identities, permissions)
	httpServer := httptest.NewServer(routes.App)
	t.Cleanup(httpServer.Close)

	mcpServer := httptest.NewServer(mcpHandler.NewHTTPHandler(config.MCPConfig{
		ServerKey:     "integration-only-server-key",
		ProfileHeader: "X-Hermes-Profile",
	}, identities, links, spaces, reminders))
	t.Cleanup(mcpServer.Close)

	adultOne := registerAndLogin(t, httpServer.URL, "Adult One", "adult-one@example.test", "081234567890")
	adultTwo := registerAndLogin(t, httpServer.URL, "Adult Two", "adult-two@example.test", "081234567891")

	adultOneSpaces := listSpaces(t, httpServer.URL, adultOne.Token)
	adultTwoSpaces := listSpaces(t, httpServer.URL, adultTwo.Token)
	personalOne := requirePersonalSpace(t, adultOneSpaces)
	personalTwo := requirePersonalSpace(t, adultTwoSpaces)
	if personalOne.UserID != adultOne.UserID || personalTwo.UserID != adultTwo.UserID {
		t.Fatalf("personal ownership mismatch: one=%+v two=%+v", personalOne, personalTwo)
	}

	sharedOne := createSharedSpace(t, httpServer.URL, adultOne.Token, "Shared Room")
	sharedTwo := createSharedSpace(t, httpServer.URL, adultOne.Token, "Shared Room")
	invitation := createInvitation(t, httpServer.URL, adultOne.Token, sharedOne.ID, adultTwo.Email)
	acceptInvitation(t, httpServer.URL, adultTwo.Token, invitation.Token)

	linkCode := issueHermesLinkCode(t, httpServer.URL, adultOne.Token)
	profile := "hermes-profile-adult-one"
	identityLink := callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "identity_link", map[string]any{
		"code": linkCode,
	})
	if identityLink.IsError {
		t.Fatalf("identity_link failed: %+v", identityLink)
	}
	if repeat := callMCPTool(t, mcpServer.URL, "hermes-profile-repeat", "integration-only-server-key", "identity_link", map[string]any{
		"code": linkCode,
	}); !repeat.IsError || mcpText(repeat) != "invalid input" {
		t.Fatalf("repeated identity link = %+v, want safe invalid input", repeat)
	}

	spacesResult := callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "space_list", map[string]any{})
	var linkedSpacesOutput mcpSpaceListOutput
	decodeStructured(t, spacesResult.StructuredOutput, &linkedSpacesOutput)
	if spacesResult.IsError || !hasSpace(linkedSpacesOutput.Spaces, personalOne.SpaceID) || !hasSpace(linkedSpacesOutput.Spaces, sharedOne.ID) || !hasSpace(linkedSpacesOutput.Spaces, sharedTwo.ID) {
		t.Fatalf("space_list = %+v", spacesResult)
	}
	memberResult := callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "space_get_members", map[string]any{
		"space": sharedOne.ID,
	})
	var memberOutput mcpSpaceMembersOutput
	decodeStructured(t, memberResult.StructuredOutput, &memberOutput)
	if memberResult.IsError || len(memberOutput.Members) != 2 || !hasUser(memberOutput.Members, adultOne.UserID) || !hasUser(memberOutput.Members, adultTwo.UserID) {
		t.Fatalf("space_get_members = %+v", memberResult)
	}

	personalReminder := decodeReminder(t, callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "reminder_create", map[string]any{
		"title":        "Personal note",
		"scheduled_at": "2026-09-21T08:00:00Z",
	}))
	personalList := callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "reminder_list", map[string]any{})
	var personalReminderOutput mcpReminderListOutput
	decodeStructured(t, personalList.StructuredOutput, &personalReminderOutput)
	if personalList.IsError || !hasReminder(personalReminderOutput.Reminders, personalReminder.ID) {
		t.Fatalf("personal reminder list = %+v", personalList)
	}
	completedPersonal := decodeReminder(t, callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "reminder_complete", map[string]any{
		"reminder_id": personalReminder.ID,
	}))
	if completedPersonal.Status != "COMPLETED" {
		t.Fatalf("completed personal reminder = %+v", completedPersonal)
	}

	sharedReminder := decodeReminder(t, callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "reminder_create", map[string]any{
		"space":        sharedOne.ID,
		"title":        "Shared task",
		"scheduled_at": "2026-09-22T08:00:00Z",
	}))
	sharedList := callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "reminder_list", map[string]any{
		"space": sharedOne.ID,
	})
	var sharedReminderOutput mcpReminderListOutput
	decodeStructured(t, sharedList.StructuredOutput, &sharedReminderOutput)
	if sharedList.IsError || !hasReminder(sharedReminderOutput.Reminders, sharedReminder.ID) {
		t.Fatalf("shared reminder list = %+v", sharedList)
	}

	if status, _ := requestJSON(t, httpServer.URL, http.MethodPost, "/api/reminders", adultTwo.Token, map[string]any{
		"space_id":     sharedOne.ID,
		"title":        "Viewer must not create",
		"scheduled_at": "2026-09-23T08:00:00Z",
	}); status != http.StatusForbidden {
		t.Fatalf("viewer reminder create status = %d, want %d", status, http.StatusForbidden)
	}

	if ambiguous := callMCPTool(t, mcpServer.URL, profile, "integration-only-server-key", "reminder_list", map[string]any{
		"space": "Shared Room",
	}); !ambiguous.IsError || mcpText(ambiguous) != "invalid input" {
		t.Fatalf("ambiguous Space selection = %+v, want safe invalid input", ambiguous)
	}
	if status, _ := requestJSON(t, httpServer.URL, http.MethodGet, "/api/reminders?space_id="+personalOne.SpaceID, adultTwo.Token, nil); status != http.StatusNotFound {
		t.Fatalf("cross-Space list status = %d, want %d", status, http.StatusNotFound)
	}

	var auditRow struct {
		ActorUserID string
		Action      string
		ResourceID  string
		Status      string
		Metadata    string
	}
	if err := db.Raw(`
		SELECT COALESCE(actor_user_id::text, '') AS actor_user_id, action, resource_id, status, metadata
		FROM audit_trails
		WHERE resource = 'reminder' AND resource_id = ?
		ORDER BY created_at DESC
		LIMIT 1`, sharedReminder.ID).Scan(&auditRow).Error; err != nil {
		t.Fatal(err)
	}
	if auditRow.ActorUserID != adultOne.UserID || auditRow.ResourceID != sharedReminder.ID || auditRow.Status != "success" {
		t.Fatalf("trusted audit row = %+v", auditRow)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(auditRow.Metadata), &metadata); err != nil {
		t.Fatalf("decode audit metadata: %v", err)
	}
	for key, want := range map[string]string{
		"source":        "mcp",
		"channel":       "whatsapp",
		"agent_profile": profile,
	} {
		if got, _ := metadata[key].(string); got != want {
			t.Errorf("audit metadata[%q] = %v, want %q", key, metadata[key], want)
		}
	}

	var persisted struct {
		SpaceID string
		Status  string
	}
	if err := db.Raw(`SELECT space_id, status FROM reminders WHERE id = ?`, sharedReminder.ID).Scan(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.SpaceID != sharedOne.ID || persisted.Status != "PENDING" {
		t.Fatalf("persisted shared reminder = %+v", persisted)
	}
	if len(adultTwoSpaces) != 1 {
		t.Fatalf("Space isolation before invitation = %+v", adultTwoSpaces)
	}
	adultTwoSpaces = listSpaces(t, httpServer.URL, adultTwo.Token)
	if !hasSpace(adultTwoSpaces, sharedOne.ID) || hasSpace(adultTwoSpaces, sharedTwo.ID) {
		t.Fatalf("Space isolation after invitation = %+v", adultTwoSpaces)
	}
}

func loadIntegrationEnv(path string) error {
	file, err := os.Open(path) // #nosec G304 -- integration reads only the worktree .env path.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "\"'")
		_ = os.Setenv(strings.TrimSpace(key), value)
	}
	return scanner.Err()
}

type registeredUser struct {
	UserID string
	Email  string
	Token  string
}

type apiResponse struct {
	Data    json.RawMessage `json:"data"`
	Error   json.RawMessage `json:"error"`
	Message string          `json:"message"`
}

type invitationResponse struct {
	Token string `json:"token"`
}

type linkCodeResponse struct {
	Code string `json:"code"`
}

type mcpWireResponse struct {
	Result *mcpWireResult `json:"result,omitempty"`
	Error  *mcpWireError  `json:"error,omitempty"`
}

type mcpWireResult struct {
	Content          []mcpWireContent `json:"content"`
	StructuredOutput json.RawMessage  `json:"structuredContent"`
	IsError          bool             `json:"isError"`
}

type mcpWireContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type mcpWireError struct {
	Message string `json:"message"`
}

type mcpReminderOutput struct {
	ID        string `json:"id"`
	SpaceID   string `json:"space_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Scheduled string `json:"scheduled_at"`
}

type mcpSpaceListOutput struct {
	Spaces []domainspace.ResolvedMembership `json:"spaces"`
}

type mcpSpaceMembersOutput struct {
	Members []domainspace.ResolvedMembership `json:"members"`
}

type mcpReminderListOutput struct {
	Reminders []mcpReminderOutput `json:"reminders"`
}

func registerAndLogin(t *testing.T, baseURL, name, email, phone string) registeredUser {
	t.Helper()
	status, response := requestJSON(t, baseURL, http.MethodPost, "/api/user/register", "", map[string]any{
		"name": name, "email": email, "phone": phone, "password": "Strong-pass1!", "birth_date": "1990-01-01",
	})
	if status != http.StatusCreated {
		t.Fatalf("register %s status=%d message=%s error=%s", email, status, response.Message, response.Error)
	}
	var user struct {
		ID string `json:"id"`
	}
	decodeData(t, response.Data, &user)
	if user.ID == "" {
		t.Fatalf("register %s missing user id", email)
	}
	status, response = requestJSON(t, baseURL, http.MethodPost, "/api/user/login", "", map[string]any{
		"identifier": email, "password": "Strong-pass1!",
	})
	if status != http.StatusOK {
		t.Fatalf("login %s status=%d message=%s error=%s", email, status, response.Message, response.Error)
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeData(t, response.Data, &auth)
	if auth.AccessToken == "" {
		t.Fatalf("login %s missing access token", email)
	}
	return registeredUser{UserID: user.ID, Email: email, Token: auth.AccessToken}
}

func listSpaces(t *testing.T, baseURL, token string) []domainspace.ResolvedMembership {
	t.Helper()
	status, response := requestJSON(t, baseURL, http.MethodGet, "/api/spaces", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list Spaces status=%d message=%s error=%s", status, response.Message, response.Error)
	}
	var memberships []domainspace.ResolvedMembership
	decodeData(t, response.Data, &memberships)
	return memberships
}

func requirePersonalSpace(t *testing.T, memberships []domainspace.ResolvedMembership) domainspace.ResolvedMembership {
	t.Helper()
	var personal domainspace.ResolvedMembership
	for _, membership := range memberships {
		if membership.SpaceType == domainspace.TypePersonal {
			if personal.ID != "" {
				t.Fatalf("multiple Personal Spaces: %+v", memberships)
			}
			personal = membership
		}
	}
	if personal.ID == "" {
		t.Fatalf("Personal Space missing: %+v", memberships)
	}
	return personal
}

func createSharedSpace(t *testing.T, baseURL, token, name string) domainspace.Space {
	t.Helper()
	status, response := requestJSON(t, baseURL, http.MethodPost, "/api/spaces", token, map[string]any{
		"name": name, "category": "family",
	})
	if status != http.StatusCreated {
		t.Fatalf("create Shared Space status=%d message=%s error=%s", status, response.Message, response.Error)
	}
	var created domainspace.Space
	decodeData(t, response.Data, &created)
	if created.ID == "" || created.Type != domainspace.TypeShared {
		t.Fatalf("created Shared Space = %+v", created)
	}
	return created
}

func createInvitation(t *testing.T, baseURL, token, spaceID, email string) invitationResponse {
	t.Helper()
	status, response := requestJSON(t, baseURL, http.MethodPost, "/api/spaces/"+spaceID+"/invitations", token, map[string]any{
		"invited_email": email, "role_name": "space_viewer",
	})
	if status != http.StatusCreated {
		t.Fatalf("create invitation status=%d message=%s error=%s", status, response.Message, response.Error)
	}
	var created invitationResponse
	decodeData(t, response.Data, &created)
	if created.Token == "" {
		t.Fatal("invitation token missing")
	}
	return created
}

func acceptInvitation(t *testing.T, baseURL, token, invitation string) {
	t.Helper()
	status, response := requestJSON(t, baseURL, http.MethodPost, "/api/invitations/accept", token, map[string]any{"token": invitation})
	if status != http.StatusOK {
		t.Fatalf("accept invitation status=%d message=%s error=%s", status, response.Message, response.Error)
	}
}

func issueHermesLinkCode(t *testing.T, baseURL, token string) string {
	t.Helper()
	status, response := requestJSON(t, baseURL, http.MethodPost, "/api/hermes/link-codes", token, nil)
	if status != http.StatusCreated {
		t.Fatalf("issue Hermes link code status=%d message=%s error=%s", status, response.Message, response.Error)
	}
	var code linkCodeResponse
	decodeData(t, response.Data, &code)
	if code.Code == "" {
		t.Fatal("Hermes link code missing")
	}
	return code.Code
}

func requestJSON(t *testing.T, baseURL string, method string, path string, token string, payload any) (int, apiResponse) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, body) // #nosec G704 -- baseURL is a local httptest server.
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req) // #nosec G704 -- request targets local httptest server.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var response apiResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, response
}

func callMCPTool(t *testing.T, serverURL, profile, key, name string, arguments map[string]any) mcpWireResult {
	t.Helper()
	callMCP(t, serverURL, profile, key, "initialize", map[string]any{
		"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "family-assistant-integration", "version": "1"},
	})
	response := callMCP(t, serverURL, profile, key, "tools/call", map[string]any{
		"name": name, "arguments": arguments,
	})
	if response.Error != nil {
		t.Fatalf("MCP %s protocol error: %s", name, response.Error.Message)
	}
	if response.Result == nil {
		t.Fatalf("MCP %s missing result", name)
	}
	return *response.Result
}

func callMCP(t *testing.T, serverURL, profile, key, method string, params map[string]any) mcpWireResponse {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/mcp", bytes.NewReader(payload)) // #nosec G704 -- serverURL is a local httptest server.
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Hermes-Profile", profile)
	req.Header.Set("X-Hermes-Channel", "whatsapp")
	res, err := http.DefaultClient.Do(req) // #nosec G704 -- request targets local httptest server.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("MCP %s status=%d", method, res.StatusCode)
	}
	var response mcpWireResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeReminder(t *testing.T, result mcpWireResult) mcpReminderOutput {
	t.Helper()
	if result.IsError || len(result.StructuredOutput) == 0 {
		t.Fatalf("MCP reminder result = %+v", result)
	}
	var reminder mcpReminderOutput
	decodeStructured(t, result.StructuredOutput, &reminder)
	if reminder.ID == "" {
		t.Fatalf("MCP reminder missing ID: %+v", result)
	}
	return reminder
}

func decodeStructured(t *testing.T, raw json.RawMessage, destination any) {
	t.Helper()
	if err := json.Unmarshal(raw, destination); err != nil {
		t.Fatalf("decode MCP structured output: %v", err)
	}
}

func decodeData(t *testing.T, raw json.RawMessage, destination any) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatal("API response missing data")
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		t.Fatalf("decode API data: %v", err)
	}
}

func mcpText(result mcpWireResult) string {
	for _, content := range result.Content {
		if content.Type == "text" {
			return content.Text
		}
	}
	return ""
}

func hasSpace(memberships []domainspace.ResolvedMembership, id string) bool {
	for _, membership := range memberships {
		if membership.SpaceID == id {
			return true
		}
	}
	return false
}

func hasUser(memberships []domainspace.ResolvedMembership, id string) bool {
	for _, membership := range memberships {
		if membership.UserID == id {
			return true
		}
	}
	return false
}

func hasReminder(reminders []mcpReminderOutput, id string) bool {
	for _, reminder := range reminders {
		if reminder.ID == id {
			return true
		}
	}
	return false
}

func resetIntegrationDatabase(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM test database: %v", err)
	}
	if err := db.Exec("DROP SCHEMA public CASCADE").Error; err != nil {
		t.Fatalf("reset test schema: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA public").Error; err != nil {
		t.Fatalf("recreate test schema: %v", err)
	}
	migration, err := migrate.New("file://../../migrations", dsn)
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = migration.Close() })
	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migration setup: %v", err)
	}
	if err := migration.Down(); err != nil && !errors.Is(err, migrate.ErrNilVersion) && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migration down: %v", err)
	}
	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migration up: %v", err)
	}
	return db
}

func assertSafeTestDSN(t *testing.T, dsn string) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" {
		t.Fatalf("non-local host")
	}
	if strings.Trim(parsed.Path, "/") != "family_assistant_test" {
		t.Fatalf("wrong database")
	}
}
