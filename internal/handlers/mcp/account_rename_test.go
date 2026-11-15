package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfaceonboarding "family-assistant/internal/interfaces/onboarding"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/pkg/config"
)

const (
	renameTestExternalID = "6285333320090@s.whatsapp.net"
	// renameAttemptedName must never appear in an error, a log attribute or a tool output other than the service's own result.
	renameAttemptedName = "Zorblax Quimby"
)

func newAccountRenameServer(t *testing.T, registrar interfaceonboarding.ServiceOnboardingInterface) *httptest.Server {
	t.Helper()
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret", IdentitySecret: scheduledTestSecret}, &resolverStub{}, nil, registrar, nil, nil, ToolServices{})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	return server
}

func callAccountRename(t *testing.T, server *httptest.Server, args map[string]any, identity *IdentityEnvelope) mcpServerResponse {
	t.Helper()
	return callScheduledTool(t, server.URL, "account_rename", args, identity)
}

func renameTestContext() context.Context {
	return WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp"})
}

func TestAccountRenameMapsInputWithTrustedIdentity(t *testing.T) {
	service := &registrarStub{renameResult: dto.AccountRenameResult{Status: "renamed", Name: "Jane Doe"}}
	got, err := AccountRename(renameTestContext(), service, AccountRenameInput{Name: " jane doe "})
	if err != nil || got != (AccountRenameOutput{Status: "renamed", Name: "Jane Doe"}) {
		t.Fatalf("output = %+v, err = %v", got, err)
	}
	want := dto.AccountRenameInput{Name: " jane doe ", Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp"}
	if service.renameCalls != 1 || service.renameInput != want {
		t.Fatalf("service input = %+v calls %d, want %+v once", service.renameInput, service.renameCalls, want)
	}
}

func TestAccountRenameRequiresTrustedExternalRequest(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"missing":           context.Background(),
		"empty provider":    WithExternalRequest(context.Background(), ExternalRequest{ExternalID: "profile-1"}),
		"empty external id": WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes"}),
	} {
		t.Run(name, func(t *testing.T) {
			service := &registrarStub{}
			_, err := AccountRename(ctx, service, AccountRenameInput{Name: "Jane Doe"})
			assertMCPCode(t, err, "unauthenticated")
			if service.renameCalls != 0 {
				t.Fatalf("service calls = %d, want 0", service.renameCalls)
			}
		})
	}
}

func TestAccountRenameNilRegistrarFailsClosed(t *testing.T) {
	_, err := AccountRename(renameTestContext(), nil, AccountRenameInput{Name: "Jane Doe"})
	assertMCPCode(t, err, "internal")
}

func TestAccountRenameUnlinkedCallerIsUnauthenticated(t *testing.T) {
	service := &registrarStub{renameErr: fmt.Errorf("rename: %w", domainidentity.ErrIdentityNotFound)}
	_, err := AccountRename(renameTestContext(), service, AccountRenameInput{Name: renameAttemptedName})
	assertMCPCode(t, err, "unauthenticated")
	if err.Error() != "authentication required" || strings.Contains(err.Error(), "Zorblax") {
		t.Fatalf("error = %q", err)
	}
}

func TestAccountRenameThroughSDKRenamedAndUnchanged(t *testing.T) {
	service := &registrarStub{renameResult: dto.AccountRenameResult{Status: "renamed", Name: "Jane Doe"}}
	server := newAccountRenameServer(t, service)

	resp := callAccountRename(t, server, map[string]any{"name": "jane  doe"}, directIdentity("nonce-rename-1"))
	if resp.Error != nil || resp.Result == nil || resp.Result.IsError {
		t.Fatalf("renamed = %+v", resp)
	}
	if got := string(resp.Result.StructuredOutput); got != `{"name":"Jane Doe","status":"renamed"}` && got != `{"status":"renamed","name":"Jane Doe"}` {
		t.Fatalf("renamed output = %s", got)
	}
	want := dto.AccountRenameInput{Name: "jane  doe", Provider: "hermes", ExternalID: renameTestExternalID, Channel: "whatsapp"}
	if service.renameCalls != 1 || service.renameInput != want {
		t.Fatalf("service input = %+v calls %d, want %+v", service.renameInput, service.renameCalls, want)
	}

	service.renameResult = dto.AccountRenameResult{Status: "unchanged", Name: "Jane Doe"}
	resp = callAccountRename(t, server, map[string]any{"name": "JANE DOE"}, directIdentity("nonce-rename-2"))
	if resp.Error != nil || resp.Result == nil || resp.Result.IsError {
		t.Fatalf("unchanged = %+v", resp)
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Result.StructuredOutput, &out); err != nil || fmt.Sprint(out) != "map[name:Jane Doe status:unchanged]" {
		t.Fatalf("unchanged output = %s (%v)", resp.Result.StructuredOutput, err)
	}
}

func TestAccountRenameIgnoresExtraArguments(t *testing.T) {
	service := &registrarStub{renameResult: dto.AccountRenameResult{Status: "renamed", Name: "Jane Doe"}}
	server := newAccountRenameServer(t, service)

	resp := callAccountRename(t, server, map[string]any{
		"name": "Jane Doe", "user_id": "victim-user", "target": "victim-user", "phone": "0812-3456-7890",
		"provider": "evil", "external_id": "victim@s.whatsapp.net", "channel": "evil",
	}, directIdentity("nonce-rename-extra"))
	if resp.Error != nil || resp.Result == nil || resp.Result.IsError {
		t.Fatalf("extra arguments = %+v", resp)
	}
	want := dto.AccountRenameInput{Name: "Jane Doe", Provider: "hermes", ExternalID: renameTestExternalID, Channel: "whatsapp"}
	if service.renameCalls != 1 || service.renameInput != want {
		t.Fatalf("service saw %+v (calls %d), want only the caller identity %+v", service.renameInput, service.renameCalls, want)
	}
}

func TestAccountRenameIsRefusedOutsideDirectMessages(t *testing.T) {
	for _, tc := range sharedChatCases {
		t.Run(tc.name, func(t *testing.T) {
			service := &registrarStub{renameResult: dto.AccountRenameResult{Status: "renamed", Name: "Jane Doe"}}
			server := newAccountRenameServer(t, service)
			refused := callAccountRename(t, server, map[string]any{"name": "Jane Doe"}, channelChatIdentity("nonce-rename-"+tc.name, tc.channel, groupTestChatID, tc.chatType))
			assertScheduledToolError(t, refused, dmRequiredMessage)
			if service.renameCalls != 0 {
				t.Fatalf("group call reached the service %d times", service.renameCalls)
			}
		})
	}
	service := &registrarStub{renameResult: dto.AccountRenameResult{Status: "renamed", Name: "Jane Doe"}}
	server := newAccountRenameServer(t, service)
	if resp := callAccountRename(t, server, map[string]any{"name": "Jane Doe"}, directIdentity("nonce-rename-dm")); resp.Result == nil || resp.Result.IsError || service.renameCalls != 1 {
		t.Fatalf("dm = %+v calls %d", resp, service.renameCalls)
	}
}

func TestAccountRenameThroughSDKUnlinkedCaller(t *testing.T) {
	service := &registrarStub{renameErr: domainidentity.ErrIdentityNotFound}
	server := newAccountRenameServer(t, service)
	resp := callAccountRename(t, server, map[string]any{"name": renameAttemptedName}, directIdentity("nonce-rename-unlinked"))
	assertScheduledToolError(t, resp, "authentication required")
	if strings.Contains(toolText(resp), "Zorblax") {
		t.Fatalf("error echoes the name: %s", toolText(resp))
	}
}

func TestAccountRenameValidationErrorNamesFieldAndNeverTheName(t *testing.T) {
	for _, reason := range []string{"must be 3 to 100 characters", "is already used by another member of your spaces"} {
		t.Run(reason, func(t *testing.T) {
			service := &registrarStub{renameErr: &serviceauthorization.ValidationError{Field: "name", Reason: reason}}
			server := newAccountRenameServer(t, service)
			resp := callAccountRename(t, server, map[string]any{"name": renameAttemptedName}, directIdentity("nonce-rename-invalid-"+reason[:4]))
			if resp.Result == nil || !resp.Result.IsError {
				t.Fatalf("response = %+v", resp)
			}
			text := toolText(resp)
			if text != "invalid input: name "+reason {
				t.Fatalf("text = %q", text)
			}
			if strings.Contains(text, "Zorblax") || strings.Contains(text, "Quimby") {
				t.Fatalf("error echoes the attempted name: %q", text)
			}
		})
	}
}

type renameToolInfo struct {
	description string
	input       map[string]any
	output      map[string]any
}

func accountRenameToolFromList(t *testing.T, registrar interfaceonboarding.ServiceOnboardingInterface) (renameToolInfo, int) {
	t.Helper()
	server := newAccountRenameServer(t, registrar)
	resp := callMCPServer(t, server.URL, "profile", "secret", "tools/list", map[string]any{})
	if resp.Error != nil || resp.Result == nil {
		t.Fatalf("tools/list: %+v", resp)
	}
	var info renameToolInfo
	count := 0
	for _, tool := range resp.Result.Tools {
		if tool.Name != "account_rename" {
			continue
		}
		count++
		info.description = tool.Description
		if err := json.Unmarshal(tool.InputSchema, &info.input); err != nil {
			t.Fatalf("input schema: %v", err)
		}
		if err := json.Unmarshal(tool.OutputSchema, &info.output); err != nil {
			t.Fatalf("output schema: %v", err)
		}
	}
	return info, count
}

func TestAccountRenameRegisteredOnceAndAbsentWithoutRegistrar(t *testing.T) {
	if _, count := accountRenameToolFromList(t, &registrarStub{}); count != 1 {
		t.Fatalf("account_rename registered %d times, want 1", count)
	}
	if _, count := accountRenameToolFromList(t, nil); count != 0 {
		t.Fatalf("account_rename registered %d times without a registrar, want 0", count)
	}
}

func TestAccountRenameSchemaIsASingleNameString(t *testing.T) {
	info, _ := accountRenameToolFromList(t, &registrarStub{})
	props, _ := info.input["properties"].(map[string]any)
	if len(props) != 1 {
		t.Fatalf("properties = %v, want only name", props)
	}
	prop, ok := props["name"].(map[string]any)
	if !ok || prop["type"] != "string" {
		t.Fatalf("name property = %v", props["name"])
	}
	required, _ := info.input["required"].([]any)
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("required = %v", required)
	}
	if _, set := info.input["additionalProperties"]; set && info.input["additionalProperties"] == false {
		t.Fatalf("additionalProperties must stay unset (identity envelope): %v", info.input["additionalProperties"])
	}
	outProps, _ := info.output["properties"].(map[string]any)
	if len(outProps) != 2 || outProps["status"] == nil || outProps["name"] == nil {
		t.Fatalf("output properties = %v, want status and name", outProps)
	}
}

func TestAccountRenameDescriptionIsTheBotManual(t *testing.T) {
	info, _ := accountRenameToolFromList(t, &registrarStub{})
	description := strings.ToLower(info.description)
	for _, phrase := range []string{
		"only the account of the person who is chatting",
		"never anyone else",
		"cannot rename another member even if the owner asks",
		"ask the user for the exact new name",
		"exactly as they typed it",
		"explicit yes before calling",
		"private chat",
		"in a group",
		"3 to 100 characters",
		"hidden or control characters",
		"< or >",
		"emoji",
		"joined emoji sequences are not",
		"title case",
		"`unchanged`",
		"nothing changes",
		"already used by another member in any of their spaces",
		"ask for a different name",
		"past records show the new name",
		"current name",
		"space names",
		"personal space",
		"not renamed",
		"no undo",
		"renaming again",
	} {
		if !strings.Contains(description, phrase) {
			t.Errorf("description lacks %q:\n%s", phrase, info.description)
		}
	}
}

func TestAccountRenameLogsNeverContainTheName(t *testing.T) {
	for _, key := range []string{"name", "Name"} {
		if safeMCPLogField(key) {
			t.Fatalf("log allowlist accepts %q", key)
		}
	}
	attrs := mcpToolLogAttrs("account_rename",
		AccountRenameInput{Name: renameAttemptedName},
		AccountRenameOutput{Status: "renamed", Name: "Zorblax Quimby"},
		time.Millisecond, errors.New("zorblax quimby private detail"))
	got := mcpLogAttrValues(attrs)
	dump := strings.ToLower(fmt.Sprint(got))
	for _, secret := range []string{"zorblax", "quimby"} {
		if strings.Contains(dump, secret) {
			t.Fatalf("log attrs contain %q: %v", secret, got)
		}
	}
	if got["output_status"] != "renamed" || got["tool_name"] != "account_rename" {
		t.Fatalf("expected the safe fields to stay logged: %v", got)
	}
	for key := range got {
		if strings.HasSuffix(key, "_name") && key != "tool_name" {
			t.Fatalf("name-like key logged: %s", key)
		}
	}
}
