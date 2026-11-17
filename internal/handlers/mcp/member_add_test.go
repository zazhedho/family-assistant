package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/pkg/config"

	"gorm.io/gorm"
)

const (
	memberAddSpaceID = "00000000-0000-0000-0000-000000000201"
	// memberAddPhone is deliberately fake; it must never appear in any output.
	memberAddPhone = "0812-3456-7890"
)

func memberAddResolver() *mcpExternalResolverStub {
	return &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", memberAddSpaceID, "Trading", domainspace.TypeShared, "role-owner"),
		}},
		permissions: []domainpermission.Permission{{Resource: "invitations", Action: "create"}},
	}
}

func memberAddPreviewResult() *dto.MemberAddResult {
	return &dto.MemberAddResult{Preview: dto.MemberAddPreview{
		DisplayName: "Budi Santoso", SpaceName: "Trading", RoleName: "space_member", PhoneTail: "890",
	}}
}

func memberAddServer(t *testing.T, stub *mcpInvitationServiceStub, resolver *mcpExternalResolverStub) *httptest.Server {
	t.Helper()
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, resolver, nil, nil, nil, nil, ToolServices{Invitation: stub})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	return server
}

func callMemberAdd(t *testing.T, server *httptest.Server, args string) mcpServerResponse {
	t.Helper()
	return callMCPServer(t, server.URL, "profile", "secret", "tools/call", map[string]any{"name": "member_add_by_phone", "arguments": json.RawMessage(args)})
}

func TestMemberAddMapsInputToServiceWithTrustedActor(t *testing.T) {
	service := &mcpInvitationServiceStub{memberAddResult: memberAddPreviewResult()}

	got, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{
		Space: " trading ", Phone: memberAddPhone, RoleName: "space_viewer", ConfirmName: "Budi Santoso",
	})
	if err != nil {
		t.Fatalf("member_add_by_phone: %v", err)
	}
	want := dto.MemberAddByPhoneInput{SpaceID: memberAddSpaceID, Phone: memberAddPhone, RoleName: "space_viewer", ConfirmName: "Budi Santoso"}
	if service.memberAddCalls != 1 || service.memberAddUserID != "user-1" || service.memberAddInput != want {
		t.Fatalf("service call = user %q input %+v calls %d, want user-1 %+v", service.memberAddUserID, service.memberAddInput, service.memberAddCalls, want)
	}
	if got.Status != "needs_confirmation" || got.DisplayName != "Budi Santoso" || got.SpaceName != "Trading" || got.RoleName != "space_member" || got.PhoneTail != "...890" {
		t.Fatalf("output = %+v", got)
	}
}

func TestMemberAddBlankRoleDefaultsToSpaceMember(t *testing.T) {
	for _, role := range []string{"", "   "} {
		service := &mcpInvitationServiceStub{memberAddResult: memberAddPreviewResult()}
		if _, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone, RoleName: role}); err != nil {
			t.Fatalf("role %q: %v", role, err)
		}
		if service.memberAddInput.RoleName != "space_member" {
			t.Fatalf("role %q sent as %q, want space_member", role, service.memberAddInput.RoleName)
		}
	}
}

// A role the caller names is passed through untouched so the service, which owns
// the role rules, can reject space_owner and anything unknown.
func TestMemberAddPassesExplicitRoleThroughUnchanged(t *testing.T) {
	service := &mcpInvitationServiceStub{memberAddErr: &serviceauthorization.ValidationError{Field: "role_name", Reason: "must be space_admin, space_member or space_viewer"}}
	_, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone, RoleName: "space_owner"})
	assertMCPCode(t, err, "invalid_input")
	if service.memberAddInput.RoleName != "space_owner" {
		t.Fatalf("role rewritten to %q", service.memberAddInput.RoleName)
	}
}

func TestMemberAddBlankSpaceResolvesLikeInvitationCreate(t *testing.T) {
	withPersonal := func() *mcpExternalResolverStub {
		resolver := memberAddResolver()
		resolver.actor.Memberships = append([]domainspace.ResolvedMembership{
			mcpMembership("member-personal", "00000000-0000-0000-0000-000000000001", "Personal", domainspace.TypePersonal, "role-owner"),
		}, resolver.actor.Memberships...)
		return resolver
	}
	for name, newResolver := range map[string]func() *mcpExternalResolverStub{"shared only": memberAddResolver, "personal and shared": withPersonal} {
		t.Run(name, func(t *testing.T) {
			invitations := &mcpInvitationServiceStub{created: &domaininvitation.Invitation{ID: "i"}}
			_, invErr := InvitationCreate(mcpExternalContext(), newResolver(), invitations, InvitationCreateInput{RoleName: "space_member"})
			service := &mcpInvitationServiceStub{memberAddResult: memberAddPreviewResult()}
			_, addErr := MemberAddByPhone(mcpExternalContext(), newResolver(), service, MemberAddByPhoneInput{Phone: memberAddPhone})
			if (invErr == nil) != (addErr == nil) || service.memberAddInput.SpaceID != invitations.createInput.SpaceID {
				t.Fatalf("member_add space %q err %v; invitation_create space %q err %v", service.memberAddInput.SpaceID, addErr, invitations.createInput.SpaceID, invErr)
			}
			if invErr != nil && service.memberAddCalls != 0 {
				t.Fatalf("refused selection still reached the service")
			}
		})
	}
}

func TestMemberAddUnknownSpaceNeverReachesService(t *testing.T) {
	service := &mcpInvitationServiceStub{memberAddResult: memberAddPreviewResult()}
	_, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{Space: "Not Mine", Phone: memberAddPhone})
	if err == nil || service.memberAddCalls != 0 {
		t.Fatalf("err = %v, service calls = %d; want a refusal before the service", err, service.memberAddCalls)
	}
}

func TestMemberAddAddedOutput(t *testing.T) {
	result := memberAddPreviewResult()
	result.Added = true
	result.Member = &domainspace.Member{ID: "member-2", UserID: "user-2"}
	service := &mcpInvitationServiceStub{memberAddResult: result}

	got, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone, ConfirmName: "Budi Santoso"})
	if err != nil {
		t.Fatalf("member_add_by_phone: %v", err)
	}
	want := MemberAddByPhoneOutput{Status: "added", DisplayName: "Budi Santoso", SpaceName: "Trading", RoleName: "space_member", PhoneTail: "...890"}
	if got != want {
		t.Fatalf("output = %+v, want %+v", got, want)
	}
}

func TestMemberAddTailIsNeverLongerThanThreeDigits(t *testing.T) {
	result := memberAddPreviewResult()
	result.Preview.PhoneTail = "628128901234"
	service := &mcpInvitationServiceStub{memberAddResult: result}
	got, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone})
	if err != nil || got.PhoneTail != "...234" {
		t.Fatalf("tail = %q, err %v; want ...234", got.PhoneTail, err)
	}
}

func TestMemberAddNilResultIsAnInternalError(t *testing.T) {
	service := &mcpInvitationServiceStub{}
	_, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone})
	assertMCPCode(t, err, "internal")
}

func TestMemberAddNilServiceAndUnlinkedActor(t *testing.T) {
	_, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), nil, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone})
	assertMCPCode(t, err, "internal")

	service := &mcpInvitationServiceStub{memberAddResult: memberAddPreviewResult()}
	_, err = MemberAddByPhone(mcpExternalContext(), &mcpExternalResolverStub{err: errors.New("x")}, service, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone})
	if err == nil || service.memberAddCalls != 0 {
		t.Fatalf("unlinked actor reached service: err=%v calls=%d", err, service.memberAddCalls)
	}
}

func TestMemberAddMapsServiceErrors(t *testing.T) {
	cases := []struct {
		name, code, message string
		err                 error
	}{
		{"forbidden", "forbidden", "forbidden", serviceauthorization.ErrForbidden},
		{"not found", "not_found", "not found", serviceauthorization.ErrNotFound},
		{"record not found", "not_found", "not found", gorm.ErrRecordNotFound},
		{"confirm_name", "invalid_input", "invalid input: confirm_name does not match", &serviceauthorization.ValidationError{Field: "confirm_name", Reason: "does not match"}},
		{"conflict", "conflict", "conflict", domaininvitation.ErrMembershipConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &mcpInvitationServiceStub{memberAddErr: fmt.Errorf("add: %w", tc.err)}
			_, err := MemberAddByPhone(mcpExternalContext(), memberAddResolver(), service, MemberAddByPhoneInput{Space: memberAddSpaceID, Phone: memberAddPhone})
			assertMCPCode(t, err, tc.code)
			if err.Error() != tc.message {
				t.Fatalf("message = %q, want %q", err.Error(), tc.message)
			}
			if strings.Contains(err.Error(), "0812") || strings.Contains(err.Error(), "3456") {
				t.Fatalf("error echoes the phone: %q", err)
			}
		})
	}
}

func TestMemberAddThroughSDKPreviewAndAdd(t *testing.T) {
	stub := &mcpInvitationServiceStub{memberAddResult: memberAddPreviewResult()}
	server := memberAddServer(t, stub, memberAddResolver())

	preview := callMemberAdd(t, server, `{"space":"Trading","phone":"`+memberAddPhone+`","role_name":"space_viewer"}`)
	if preview.Error != nil || preview.Result == nil || preview.Result.IsError {
		t.Fatalf("preview = %+v", preview)
	}
	var out map[string]any
	if err := json.Unmarshal(preview.Result.StructuredOutput, &out); err != nil {
		t.Fatalf("structured output: %v", err)
	}
	want := map[string]any{"status": "needs_confirmation", "display_name": "Budi Santoso", "space_name": "Trading", "role_name": "space_member", "phone_tail": "...890"}
	if fmt.Sprint(out) != fmt.Sprint(want) {
		t.Fatalf("preview output = %v, want %v", out, want)
	}
	rawJSON := string(preview.Result.StructuredOutput) + toolText(preview)
	for _, leaked := range []string{"0812", "3456", "7890", "81234567890", "628123456789"} {
		if strings.Contains(rawJSON, leaked) {
			t.Fatalf("preview leaks %q: %s", leaked, rawJSON)
		}
	}
	if stub.memberAddInput.SpaceID != memberAddSpaceID || stub.memberAddInput.RoleName != "space_viewer" || stub.memberAddInput.ConfirmName != "" || stub.memberAddUserID != "user-1" {
		t.Fatalf("service input = user %q %+v", stub.memberAddUserID, stub.memberAddInput)
	}

	added := memberAddPreviewResult()
	added.Added = true
	stub.memberAddResult = added
	resp := callMemberAdd(t, server, `{"space":"Trading","phone":"`+memberAddPhone+`","confirm_name":"Budi Santoso"}`)
	if resp.Error != nil || resp.Result == nil || resp.Result.IsError || !strings.Contains(string(resp.Result.StructuredOutput), `"status":"added"`) {
		t.Fatalf("add = %+v", resp)
	}
	if stub.memberAddInput.ConfirmName != "Budi Santoso" || stub.memberAddInput.RoleName != "space_member" {
		t.Fatalf("service input = %+v", stub.memberAddInput)
	}
}

func TestMemberAddThroughSDKRejectsWithMappedError(t *testing.T) {
	stub := &mcpInvitationServiceStub{memberAddErr: &serviceauthorization.ValidationError{Field: "confirm_name", Reason: "does not match"}}
	server := memberAddServer(t, stub, memberAddResolver())
	resp := callMemberAdd(t, server, `{"space":"Trading","phone":"`+memberAddPhone+`","confirm_name":"Wrong"}`)
	if resp.Result == nil || !resp.Result.IsError || !strings.Contains(toolText(resp), "confirm_name") || strings.Contains(toolText(resp), "3456") {
		t.Fatalf("rejection = %+v", resp)
	}
}

func TestMemberAddIsRefusedInGroupChats(t *testing.T) {
	for _, chatType := range []string{"group", "channel", "thread"} {
		t.Run(chatType, func(t *testing.T) {
			server, services := newGroupTestServer(t, nil)
			identity := channelChatIdentity("nonce-member-add-"+chatType, "whatsapp", groupTestChatID, chatType)
			resp := callScheduledTool(t, server.URL, "member_add_by_phone", map[string]any{"space": scheduledTestSpaceID, "phone": memberAddPhone}, identity)
			assertScheduledToolError(t, resp, dmRequiredMessage)
			if services.invitation.memberAddCalls != 0 {
				t.Fatalf("group call reached the service %d times", services.invitation.memberAddCalls)
			}
		})
	}
}

func TestMemberAddIsAllowedInDirectMessage(t *testing.T) {
	server, services := newGroupTestServer(t, nil)
	services.invitation.memberAddResult = memberAddPreviewResult()
	resp := callScheduledTool(t, server.URL, "member_add_by_phone", map[string]any{"space": scheduledTestSpaceID, "phone": memberAddPhone}, directIdentity("nonce-member-add-dm"))
	if resp.Result == nil || resp.Result.IsError || services.invitation.memberAddCalls != 1 {
		t.Fatalf("DM call = %+v calls=%d", resp, services.invitation.memberAddCalls)
	}
}

func TestMemberAddActorIsNeverAnArgument(t *testing.T) {
	stub := &mcpInvitationServiceStub{memberAddResult: memberAddPreviewResult()}
	server := memberAddServer(t, stub, memberAddResolver())
	resp := callMemberAdd(t, server, `{"space":"Trading","phone":"`+memberAddPhone+`","user_id":"attacker","actor_user_id":"attacker"}`)
	if resp.Result != nil && resp.Result.IsError {
		t.Fatalf("extra arguments rejected: %+v", resp)
	}
	if stub.memberAddUserID != "user-1" {
		t.Fatalf("actor = %q, want the resolved user-1", stub.memberAddUserID)
	}
}

type memberAddToolInfo struct {
	description string
	input       map[string]any
}

func memberAddToolFromList(t *testing.T, services ToolServices) (memberAddToolInfo, int) {
	t.Helper()
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret"}, memberAddResolver(), nil, nil, nil, nil, services)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	resp := callMCPServer(t, server.URL, "profile", "secret", "tools/list", map[string]any{})
	if resp.Error != nil || resp.Result == nil {
		t.Fatalf("tools/list: %+v", resp)
	}
	var info memberAddToolInfo
	count := 0
	for _, tool := range resp.Result.Tools {
		if tool.Name != "member_add_by_phone" {
			continue
		}
		count++
		info.description = tool.Description
		if err := json.Unmarshal(tool.InputSchema, &info.input); err != nil {
			t.Fatalf("input schema: %v", err)
		}
	}
	return info, count
}

func TestMemberAddRegisteredOnceAndAbsentWithoutService(t *testing.T) {
	if _, count := memberAddToolFromList(t, ToolServices{Invitation: &mcpInvitationServiceStub{}}); count != 1 {
		t.Fatalf("member_add_by_phone registered %d times, want 1", count)
	}
	if _, count := memberAddToolFromList(t, ToolServices{}); count != 0 {
		t.Fatalf("member_add_by_phone registered %d times without an invitation service, want 0", count)
	}
}

func TestMemberAddSchemaInputsAreStrings(t *testing.T) {
	info, _ := memberAddToolFromList(t, ToolServices{Invitation: &mcpInvitationServiceStub{}})
	props, _ := info.input["properties"].(map[string]any)
	if len(props) != 4 {
		t.Fatalf("properties = %v, want exactly space, phone, role_name, confirm_name", props)
	}
	for _, name := range []string{"space", "phone", "role_name", "confirm_name"} {
		prop, ok := props[name].(map[string]any)
		if !ok || prop["type"] != "string" || strings.TrimSpace(fmt.Sprint(prop["description"])) == "" {
			t.Fatalf("property %s = %v, want a described string", name, props[name])
		}
	}
	required, _ := info.input["required"].([]any)
	if len(required) != 1 || required[0] != "phone" {
		t.Fatalf("required = %v, want only phone", required)
	}
	if desc := fmt.Sprint(props["phone"].(map[string]any)["description"]); !strings.Contains(desc, "0812-3456-7890") {
		t.Fatalf("phone description lacks an example: %q", desc)
	}
	if desc := strings.ToLower(fmt.Sprint(props["space"].(map[string]any)["description"])); !strings.Contains(desc, "always pass") || !strings.Contains(desc, "personal space") {
		t.Fatalf("space description lacks the always-pass rule: %q", desc)
	}
	desc := fmt.Sprint(props["role_name"].(map[string]any)["description"])
	for _, role := range []string{"space_admin", "space_member", "space_editor", "space_viewer"} {
		if !strings.Contains(desc, role) {
			t.Fatalf("role_name description %q lacks %q", desc, role)
		}
	}
}

func TestMemberAddDescriptionIsTheBotManual(t *testing.T) {
	info, _ := memberAddToolFromList(t, ToolServices{Invitation: &mcpInvitationServiceStub{}})
	description := strings.ToLower(info.description)
	for _, phrase := range []string{
		"owner or admin", "private chat", "already registered", "approved", "hermes",
		"without confirm_name", "show", "explicit yes", "never chain", "exactly", "never guess",
		"not found", "must dm the bot", "already a member", "immediately", "space_member",
		"invitation_create", "no account yet",
		"always pass space", "personal space", "does not mean the person is unregistered",
	} {
		if !strings.Contains(description, phrase) {
			t.Errorf("description lacks %q:\n%s", phrase, info.description)
		}
	}
}

func TestMemberAddLogsNeverContainPhoneNameOrConfirmation(t *testing.T) {
	attrs := mcpToolLogAttrs("member_add_by_phone",
		MemberAddByPhoneInput{Space: "Trading", Phone: memberAddPhone, RoleName: "space_viewer", ConfirmName: "Budi Santoso"},
		MemberAddByPhoneOutput{Status: "added", DisplayName: "Budi Santoso", SpaceName: "Trading", RoleName: "space_viewer", PhoneTail: "...890"},
		time.Millisecond, nil)
	got := mcpLogAttrValues(attrs)
	dump := fmt.Sprint(got)
	for _, secret := range []string{"0812", "3456", "7890", "890", "Budi", "Santoso", "Trading"} {
		if strings.Contains(dump, secret) {
			t.Fatalf("log attrs contain %q: %v", secret, got)
		}
	}
	if got["input_role_name"] != "space_viewer" || got["output_status"] != "added" {
		t.Fatalf("expected the safe fields to stay logged: %v", got)
	}
	for key := range got {
		if strings.Contains(key, "phone") || strings.Contains(key, "confirm") || strings.Contains(key, "name") && key != "tool_name" && !strings.Contains(key, "role_name") {
			t.Fatalf("sensitive key logged: %s", key)
		}
	}
}
