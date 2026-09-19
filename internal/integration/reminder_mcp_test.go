package integration

import (
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

	mcphandler "family-assistant/internal/handlers/mcp"
	auditRepo "family-assistant/internal/repositories/audit"
	familyMemberRepo "family-assistant/internal/repositories/familymember"
	permissionRepo "family-assistant/internal/repositories/permission"
	reminderRepo "family-assistant/internal/repositories/reminder"
	auditService "family-assistant/internal/services/audit"
	authorizationService "family-assistant/internal/services/authorization"
	identityService "family-assistant/internal/services/identity"
	permissionService "family-assistant/internal/services/permission"
	reminderService "family-assistant/internal/services/reminder"
	"family-assistant/pkg/config"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	testMCPServerKey = "integration-only-server-key"

	familyAID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	familyBID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	userA1ID = "00000000-0000-4000-8000-000000000101"
	userA2ID = "00000000-0000-4000-8000-000000000102"
	userA3ID = "00000000-0000-4000-8000-000000000103"
	userB1ID = "00000000-0000-4000-8000-000000000104"

	memberA1ID = "00000000-0000-4000-8000-000000000201"
	memberA2ID = "00000000-0000-4000-8000-000000000202"
	memberA3ID = "00000000-0000-4000-8000-000000000203"
	memberB1ID = "00000000-0000-4000-8000-000000000204"
)

type reminderMCPFixture struct {
	db       *gorm.DB
	familyA  string
	familyB  string
	memberA1 string
	memberA2 string
	memberA3 string
	memberB1 string
	userA1   string
}

type mcpWireClient struct {
	endpoint string
	profile  string
	key      string
	client   *http.Client
}

type mcpWireResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	Result  *mcpWireToolResult `json:"result,omitempty"`
	Error   *mcpWireError      `json:"error,omitempty"`
}

type mcpWireToolResult struct {
	Content          []mcpWireContent `json:"content"`
	StructuredOutput json.RawMessage  `json:"structuredContent"`
	IsError          bool             `json:"isError"`
}

type mcpWireContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type mcpWireError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

type reminderMCPOutput struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
	ScheduledAt string `json:"scheduled_at"`
}

func TestReminderMCP(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FAMILY_ASSISTANT_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("FAMILY_ASSISTANT_TEST_DATABASE_URL is not set")
	}
	assertSafeTestDSN(t, dsn)

	db := resetMCPDatabase(t, dsn)
	fixture := seedMCPFixture(t, db)

	members := familyMemberRepo.NewRepository(db)
	permissions := permissionService.NewPermissionService(permissionRepo.NewPermissionRepo(db))
	identity := identityService.NewResolver(members, permissions)
	audit := auditService.NewAuditService(auditRepo.NewAuditRepo(db))
	reminders := reminderService.NewReminderService(
		reminderRepo.NewRepository(db), members, authorizationService.NewAuthorizer(), audit,
	)

	server := httptest.NewServer(mcphandler.NewHTTPHandler(config.MCPConfig{
		ServerKey:     testMCPServerKey,
		ProfileHeader: "X-Hermes-Profile",
	}, identity, reminders))
	t.Cleanup(server.Close)

	badKey := newMCPWireClient(server.URL+"/mcp", "profile_parent_a1", "wrong-key")
	if status, body := badKey.post(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
			"clientInfo": map[string]string{"name": "integration", "version": "1"},
		},
	}); status != http.StatusUnauthorized || !strings.Contains(string(body), "authentication required") {
		t.Fatalf("bad bearer status/body = %d/%s", status, body)
	}

	parentA1 := newMCPWireClient(server.URL+"/mcp?member_id="+fixture.memberA2+"&family_id="+fixture.familyB+"&role=admin", "profile_parent_a1", testMCPServerKey)
	parentA2 := newMCPWireClient(server.URL+"/mcp", "profile_parent_a2", testMCPServerKey)
	childA3 := newMCPWireClient(server.URL+"/mcp", "profile_child_a3", testMCPServerKey)
	parentB1 := newMCPWireClient(server.URL+"/mcp", "profile_parent_b1", testMCPServerKey)

	own := parentA1.callTool(t, "reminder_create", map[string]any{
		"title":        "A1 own reminder",
		"scheduled_at": "2026-09-20T08:00:00Z",
		"scope":        "PERSONAL",
	})
	ownReminder := decodeReminderOutput(t, own)
	if own.IsError || ownReminder.Title != "A1 own reminder" || ownReminder.Scope != "PERSONAL" || ownReminder.Status != "PENDING" {
		t.Fatalf("parent A1 own create = %+v", own)
	}

	child := parentA1.callTool(t, "reminder_create", map[string]any{
		"title":            "A3 school pickup",
		"scheduled_at":     "2026-09-21T08:00:00Z",
		"scope":            "PERSONAL",
		"target_member_id": fixture.memberA3,
	})
	childReminder := decodeReminderOutput(t, child)
	if child.IsError || childReminder.Title != "A3 school pickup" || childReminder.Scope != "PERSONAL" || childReminder.Status != "PENDING" {
		t.Fatalf("parent A1 child create = %+v", child)
	}

	childList := parentA1.callTool(t, "reminder_list", map[string]any{
		"scope":            "PERSONAL",
		"target_member_id": fixture.memberA3,
	})
	if childList.IsError {
		t.Fatalf("parent A1 child list returned error: %+v", childList)
	}
	var listed []reminderMCPOutput
	decodeStructured(t, childList.StructuredOutput, &listed)
	if !containsReminder(listed, childReminder.ID) {
		t.Fatalf("child list = %+v, want reminder %s", listed, childReminder.ID)
	}

	expectMCPToolError(t, parentA1.callTool(t, "reminder_list", map[string]any{
		"scope":            "PERSONAL",
		"target_member_id": fixture.memberA2,
	}), "forbidden")
	expectMCPToolError(t, childA3.callTool(t, "reminder_list", map[string]any{
		"scope":            "PERSONAL",
		"target_member_id": fixture.memberA1,
	}), "forbidden")

	expectMCPToolError(t, parentB1.callTool(t, "reminder_complete", map[string]any{
		"reminder_id": childReminder.ID,
	}), "not found")

	family := parentA1.callTool(t, "reminder_create", map[string]any{
		"title":        "Family dinner",
		"scheduled_at": "2026-09-22T11:00:00Z",
		"scope":        "FAMILY",
	})
	familyReminder := decodeReminderOutput(t, family)
	if family.IsError || familyReminder.Scope != "FAMILY" {
		t.Fatalf("family create = %+v", family)
	}
	familyList := parentA2.callTool(t, "reminder_list", map[string]any{"scope": "FAMILY"})
	if familyList.IsError {
		t.Fatalf("parent A2 family list returned error: %+v", familyList)
	}
	var familyReminders []reminderMCPOutput
	decodeStructured(t, familyList.StructuredOutput, &familyReminders)
	if !containsReminder(familyReminders, familyReminder.ID) {
		t.Fatalf("family list = %+v, want reminder %s", familyReminders, familyReminder.ID)
	}

	completed := parentA1.callTool(t, "reminder_complete", map[string]any{"reminder_id": ownReminder.ID})
	completedReminder := decodeReminderOutput(t, completed)
	if completed.IsError || completedReminder.Status != "COMPLETED" || completedReminder.ID != ownReminder.ID {
		t.Fatalf("complete = %+v", completed)
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
		LIMIT 1`, ownReminder.ID).Scan(&auditRow).Error; err != nil {
		t.Fatalf("read completion audit: %v", err)
	}
	if auditRow.ActorUserID != fixture.userA1 || auditRow.Action != "update" || auditRow.Status != "success" {
		t.Fatalf("audit row = %+v", auditRow)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(auditRow.Metadata), &metadata); err != nil {
		t.Fatalf("decode audit metadata %q: %v", auditRow.Metadata, err)
	}
	for key, want := range map[string]string{
		"source":        "mcp",
		"channel":       "whatsapp",
		"agent_profile": "profile_parent_a1",
	} {
		if got, _ := metadata[key].(string); got != want {
			t.Errorf("audit metadata[%q] = %v, want %q", key, metadata[key], want)
		}
	}

	var stored struct {
		FamilyID          string
		OwnerMemberID     string
		CreatedByMemberID string
		Status            string
	}
	if err := db.Raw(`SELECT family_id, owner_member_id, created_by_member_id, status FROM reminders WHERE id = ?`, ownReminder.ID).Scan(&stored).Error; err != nil {
		t.Fatalf("read own reminder: %v", err)
	}
	if stored.FamilyID != fixture.familyA || stored.OwnerMemberID != fixture.memberA1 || stored.CreatedByMemberID != fixture.memberA1 || stored.Status != "COMPLETED" {
		t.Fatalf("stored own reminder = %+v", stored)
	}
}

func assertSafeTestDSN(t *testing.T, dsn string) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	if parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" {
		t.Fatalf("refusing to reset non-local test database host %q", parsed.Hostname())
	}
	if !strings.HasSuffix(strings.Trim(parsed.Path, "/"), "_test") {
		t.Fatalf("refusing to reset database without _test suffix: %q", parsed.Path)
	}
}

func resetMCPDatabase(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM test database: %v", err)
	}
	// This test only accepts a local database with a _test suffix above.
	// Recreate its schema so a failed migration reset cannot poison the next run.
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
	defer func() {
		_, _ = migration.Close()
	}()
	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedMCPFixture(t *testing.T, db *gorm.DB) reminderMCPFixture {
	t.Helper()
	var roleIDs struct {
		Parent string
		Child  string
	}
	if err := db.Raw(`SELECT
		(SELECT id FROM roles WHERE name = 'parent') AS parent,
		(SELECT id FROM roles WHERE name = 'child') AS child`).Scan(&roleIDs).Error; err != nil {
		t.Fatalf("read seeded roles: %v", err)
	}
	if roleIDs.Parent == "" || roleIDs.Child == "" {
		t.Fatalf("missing seeded family roles: %+v", roleIDs)
	}

	if err := db.Exec(`INSERT INTO families (id, name, status) VALUES
		(?, 'Family A', 'ACTIVE'), (?, 'Family B', 'ACTIVE')`, familyAID, familyBID).Error; err != nil {
		t.Fatalf("seed families: %v", err)
	}
	if err := db.Exec(`INSERT INTO users (id, name, email, password, role, role_id, hermes_profile_id) VALUES
		(?, 'Parent A1', 'parent-a1@example.test', 'integration-only', 'parent', ?, 'profile_parent_a1'),
		(?, 'Parent A2', 'parent-a2@example.test', 'integration-only', 'parent', ?, 'profile_parent_a2'),
		(?, 'Child A3', 'child-a3@example.test', 'integration-only', 'child', ?, 'profile_child_a3'),
		(?, 'Parent B1', 'parent-b1@example.test', 'integration-only', 'parent', ?, 'profile_parent_b1')`,
		userA1ID, roleIDs.Parent, userA2ID, roleIDs.Parent, userA3ID, roleIDs.Child, userB1ID, roleIDs.Parent).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if err := db.Exec(`INSERT INTO family_members (id, family_id, user_id, role_id, status) VALUES
		(?, ?, ?, ?, 'ACTIVE'),
		(?, ?, ?, ?, 'ACTIVE'),
		(?, ?, ?, ?, 'ACTIVE'),
		(?, ?, ?, ?, 'ACTIVE')`,
		memberA1ID, familyAID, userA1ID, roleIDs.Parent,
		memberA2ID, familyAID, userA2ID, roleIDs.Parent,
		memberA3ID, familyAID, userA3ID, roleIDs.Child,
		memberB1ID, familyBID, userB1ID, roleIDs.Parent).Error; err != nil {
		t.Fatalf("seed family members: %v", err)
	}
	return reminderMCPFixture{
		db: db, familyA: familyAID, familyB: familyBID,
		memberA1: memberA1ID, memberA2: memberA2ID, memberA3: memberA3ID, memberB1: memberB1ID,
		userA1: userA1ID,
	}
}

func newMCPWireClient(endpoint, profile, key string) *mcpWireClient {
	return &mcpWireClient{endpoint: endpoint, profile: profile, key: key, client: http.DefaultClient}
}

func (c *mcpWireClient) callTool(t *testing.T, name string, arguments map[string]any) mcpWireToolResult {
	t.Helper()
	status, body := c.post(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
			"clientInfo": map[string]string{"name": "family-assistant-integration", "version": "1"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize %s status/body = %d/%s", c.profile, status, body)
	}
	var initialize mcpWireResponse
	if err := json.Unmarshal(body, &initialize); err != nil || initialize.Error != nil {
		t.Fatalf("initialize %s response = %s (decode=%v)", c.profile, body, err)
	}

	status, body = c.post(t, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	})
	if status != http.StatusOK {
		t.Fatalf("tool %s/%s status/body = %d/%s", c.profile, name, status, body)
	}
	var response mcpWireResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("tool %s/%s response = %s: %v", c.profile, name, body, err)
	}
	if response.Error != nil {
		t.Fatalf("tool %s/%s protocol error = %+v", c.profile, name, response.Error)
	}
	if response.Result == nil {
		t.Fatalf("tool %s/%s missing result: %s", c.profile, name, body)
	}
	return *response.Result
}

func (c *mcpWireClient) post(t *testing.T, payload map[string]any) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal MCP request: %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.endpoint, bytes.NewReader(body)) // #nosec G704 -- endpoint is the local httptest server created by this test.
	if err != nil {
		t.Fatalf("build MCP request: %v", err)
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.key)
	request.Header.Set("X-Hermes-Profile", c.profile)
	request.Header.Set("X-Hermes-Channel", "whatsapp")
	response, err := c.client.Do(request) // #nosec G704 -- request targets the local httptest server created by this test.
	if err != nil {
		t.Fatalf("MCP request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read MCP response: %v", err)
	}
	return response.StatusCode, responseBody
}

func decodeReminderOutput(t *testing.T, response mcpWireToolResult) reminderMCPOutput {
	t.Helper()
	if len(response.StructuredOutput) == 0 {
		t.Fatalf("missing structured reminder output: %+v", response)
	}
	var output reminderMCPOutput
	decodeStructured(t, response.StructuredOutput, &output)
	return output
}

func decodeStructured(t *testing.T, raw json.RawMessage, destination any) {
	t.Helper()
	if err := json.Unmarshal(raw, destination); err != nil {
		t.Fatalf("decode structured output %s: %v", raw, err)
	}
}

func expectMCPToolError(t *testing.T, response mcpWireToolResult, wantMessage string) {
	t.Helper()
	if !response.IsError {
		t.Fatalf("expected MCP tool error %q, got success: %+v", wantMessage, response)
	}
	var messages []string
	for _, content := range response.Content {
		if content.Type == "text" {
			messages = append(messages, content.Text)
		}
	}
	got := strings.Join(messages, "\n")
	if got != wantMessage {
		t.Fatalf("MCP error message = %q, want %q", got, wantMessage)
	}
}

func containsReminder(reminders []reminderMCPOutput, id string) bool {
	for _, reminder := range reminders {
		if reminder.ID == id {
			return true
		}
	}
	return false
}
