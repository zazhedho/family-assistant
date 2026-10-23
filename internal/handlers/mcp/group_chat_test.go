package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainspace "family-assistant/internal/domain/space"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	"family-assistant/pkg/config"
)

const (
	groupTestPersonalSpaceID = "00000000-0000-0000-0000-000000000301"
	groupTestPersonalMember  = "00000000-0000-0000-0000-000000000302"
	groupTestChatID          = "120363@g.us"
	dmRequiredMessage        = "this action must be done in a private chat with the assistant"
)

type groupSpaceLookupStub struct {
	spaceID string
	found   bool
	err     error
	calls   int
	channel string
	chatID  string
}

func (s *groupSpaceLookupStub) ActiveSpaceIDForChat(_ context.Context, channel, chatID string) (string, bool, error) {
	s.calls++
	s.channel, s.chatID = channel, chatID
	return s.spaceID, s.found, s.err
}

var _ interfaceidentity.GroupSpaceLookup = (*groupSpaceLookupStub)(nil)

// groupResolver gives the sender a Personal Space and a shared Family Space.
func groupResolver() *resolverStub {
	resolver := scheduledResolver(scheduledTestSpaceID)
	resolver.actor.Memberships = append(resolver.actor.Memberships, domainspace.ResolvedMembership{
		ID: groupTestPersonalMember, SpaceID: groupTestPersonalSpaceID, SpaceName: "Personal", SpaceType: domainspace.TypePersonal,
		UserID: "user-1", RoleID: "role-2", RoleName: "space_owner", Status: domainspace.StatusActive,
	})
	return resolver
}

func chatIdentity(nonce, chatID, chatType string) *IdentityEnvelope {
	return channelChatIdentity(nonce, "whatsapp", chatID, chatType)
}

func channelChatIdentity(nonce, channel, chatID, chatType string) *IdentityEnvelope {
	envelope := IdentityEnvelope{
		Version: identityEnvelopeVersion, Provider: "hermes", ExternalID: "6285333320090@s.whatsapp.net", Channel: channel,
		ChatID: chatID, ChatType: chatType, IssuedAt: time.Now().Unix(), Nonce: nonce,
	}
	envelope.Signature = signIdentityEnvelope(scheduledTestSecret, envelope)
	return &envelope
}

func groupIdentity(nonce string) *IdentityEnvelope {
	return chatIdentity(nonce, groupTestChatID, "group")
}

func directIdentity(nonce string) *IdentityEnvelope {
	return chatIdentity(nonce, "6285333320090@s.whatsapp.net", "dm")
}

type groupTestServices struct {
	scheduled  *scheduledMessageServiceStub
	space      *mcpSpaceServiceStub
	registrar  *registrarStub
	link       *identityLinkServiceStub
	invitation *mcpInvitationServiceStub
}

func newGroupTestServer(t *testing.T, lookup interfaceidentity.GroupSpaceLookup) (*httptest.Server, groupTestServices) {
	t.Helper()
	services := groupTestServices{
		scheduled: &scheduledMessageServiceStub{},
		space: &mcpSpaceServiceStub{
			spaces: []domainspace.ResolvedMembership{{
				ID: "member-1", SpaceID: scheduledTestSpaceID, SpaceName: "Family", SpaceType: domainspace.TypeShared, SpaceCategory: "family",
				UserID: "user-1", UserName: "Sam", UserEmail: "sam@example.com", RoleID: "role-1", RoleName: "space_member", Status: domainspace.StatusActive,
			}},
		},
		registrar: &registrarStub{},
		link: &identityLinkServiceStub{identity: &domainidentity.ExternalIdentity{
			ID: "identity-1", UserID: "user-1", Provider: "hermes", ExternalID: "6285333320090@s.whatsapp.net",
		}},
		invitation: &mcpInvitationServiceStub{
			created:     &domaininvitation.Invitation{ID: "invitation-1", SpaceID: scheduledTestSpaceID},
			createToken: "invite-token",
		},
	}
	services.space.members = services.space.spaces
	services.space.member = &services.space.spaces[0]
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret", IdentitySecret: scheduledTestSecret}, groupResolver(), services.link, services.registrar, services.space, nil, ToolServices{
		ScheduledMessage: services.scheduled, GroupBinder: services.scheduled, Invitation: services.invitation, GroupSpaces: lookup,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	return server, services
}

func TestGroupBlankSpaceUsesBoundSpace(t *testing.T) {
	lookup := &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true}
	server, services := newGroupTestServer(t, lookup)

	response := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{}, groupIdentity("nonce-bound"))
	if response.Result == nil || response.Result.IsError {
		t.Fatalf("scheduled_message_targets failed: %+v", response)
	}
	if lookup.calls != 1 || lookup.channel != "whatsapp" || lookup.chatID != groupTestChatID {
		t.Fatalf("lookup = %+v; want one call for the verified group", lookup)
	}
	if services.scheduled.targetCalls != 1 || services.scheduled.targetSpace != scheduledTestSpaceID {
		t.Fatalf("targets space = %q calls=%d; want bound Space %q", services.scheduled.targetSpace, services.scheduled.targetCalls, scheduledTestSpaceID)
	}
}

func TestGroupBlankSpaceUsesBoundSpaceForSpaceGetMembers(t *testing.T) {
	lookup := &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true}
	server, services := newGroupTestServer(t, lookup)

	response := callScheduledTool(t, server.URL, "space_get_members", map[string]any{}, groupIdentity("nonce-members"))
	if response.Result == nil || response.Result.IsError || services.space.membersSpace != scheduledTestSpaceID {
		t.Fatalf("space_get_members space=%q response=%+v; want bound Space", services.space.membersSpace, response)
	}
}

func TestGroupBlankSpaceFailsWhenGroupIsNotBound(t *testing.T) {
	const wantMessage = "space is required in a group chat that is not bound to a Space"
	for name, lookup := range map[string]interfaceidentity.GroupSpaceLookup{
		"unbound group":  &groupSpaceLookupStub{},
		"blank bound id": &groupSpaceLookupStub{found: true},
		"nil lookup":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			server, services := newGroupTestServer(t, lookup)
			response := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{}, groupIdentity("nonce-unbound-"+name))
			assertScheduledToolError(t, response, wantMessage)
			if services.scheduled.targetCalls != 0 {
				t.Fatalf("unbound group reached service %d times", services.scheduled.targetCalls)
			}
		})
	}
}

func TestGroupBlankSpaceMapsLookupFailureToInternalError(t *testing.T) {
	server, services := newGroupTestServer(t, &groupSpaceLookupStub{err: errors.New("db down")})
	response := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{}, groupIdentity("nonce-lookup-error"))
	assertScheduledToolError(t, response, ErrMCPInternal.Error())
	if response.Result != nil && len(response.Result.Content) > 0 && strings.Contains(response.Result.Content[0].Text, "db down") {
		t.Fatalf("lookup error leaked: %+v", response.Result.Content)
	}
	if services.scheduled.targetCalls != 0 {
		t.Fatal("lookup failure reached service")
	}
}

func TestGroupBoundSpaceStillRequiresSenderMembership(t *testing.T) {
	server, services := newGroupTestServer(t, &groupSpaceLookupStub{spaceID: "00000000-0000-0000-0000-000000000399", found: true})
	response := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{}, groupIdentity("nonce-non-member"))
	assertScheduledToolError(t, response, ErrMCPNotFound.Error())
	if services.scheduled.targetCalls != 0 {
		t.Fatal("non-member reached service")
	}
}

func TestGroupExplicitSpaceSelectorIsHonored(t *testing.T) {
	for name, selector := range map[string]string{
		"personal by id":   groupTestPersonalSpaceID,
		"personal by name": "Personal",
		"shared by name":   "family",
	} {
		t.Run(name, func(t *testing.T) {
			lookup := &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true}
			server, services := newGroupTestServer(t, lookup)
			response := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{"space": selector}, groupIdentity("nonce-explicit-"+name))
			if response.Result == nil || response.Result.IsError {
				t.Fatalf("explicit selector failed: %+v", response)
			}
			if lookup.calls != 0 {
				t.Fatalf("explicit selector consulted the group binding %d times", lookup.calls)
			}
			want := selector
			if name == "personal by name" {
				want = groupTestPersonalSpaceID
			}
			if name == "shared by name" {
				want = scheduledTestSpaceID
			}
			if services.scheduled.targetSpace != want {
				t.Fatalf("space = %q, want %q", services.scheduled.targetSpace, want)
			}
		})
	}
}

func TestDirectMessageBlankSpaceStillSelectsPersonal(t *testing.T) {
	lookup := &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true}
	server, services := newGroupTestServer(t, lookup)
	response := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{}, directIdentity("nonce-dm-blank"))
	if response.Result == nil || response.Result.IsError {
		t.Fatalf("DM blank selector failed: %+v", response)
	}
	if lookup.calls != 0 || services.scheduled.targetSpace != groupTestPersonalSpaceID {
		t.Fatalf("DM space = %q lookup calls=%d; want Personal Space without lookup", services.scheduled.targetSpace, lookup.calls)
	}
}

func TestUnverifiedRequestNeverCountsAsGroup(t *testing.T) {
	lookup := &groupSpaceLookupStub{}
	ctx := withGroupSpaceLookup(WithExternalRequest(context.Background(), ExternalRequest{
		Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp", ChatID: groupTestChatID, ChatType: "group",
	}), lookup)
	actor, err := selectSpaceActor(ctx, groupResolver(), "")
	if err != nil || actor.SpaceID != groupTestPersonalSpaceID || lookup.calls != 0 {
		t.Fatalf("unverified selection = %+v, %v, lookup calls=%d; want Personal Space", actor, err, lookup.calls)
	}
	if err := requireDirectMessage(ctx); err != nil {
		t.Fatalf("unverified request treated as group: %v", err)
	}
}

func TestSpaceChannelBindRequiresExplicitSpaceInGroup(t *testing.T) {
	lookup := &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true}
	server, services := newGroupTestServer(t, lookup)

	blank := callScheduledTool(t, server.URL, "space_channel_bind", map[string]any{}, groupIdentity("nonce-bind-blank"))
	assertScheduledToolError(t, blank, "space is required to bind a group chat to a Space")
	if services.scheduled.bindCalls != 0 || lookup.calls != 0 {
		t.Fatalf("blank bind reached service=%d lookup=%d", services.scheduled.bindCalls, lookup.calls)
	}

	explicit := callScheduledTool(t, server.URL, "space_channel_bind", map[string]any{"space": "Family"}, groupIdentity("nonce-bind-explicit"))
	if explicit.Result == nil || explicit.Result.IsError || services.scheduled.bindCalls != 1 || services.scheduled.bindInput.Space != scheduledTestSpaceID {
		t.Fatalf("explicit bind = %+v input=%+v", explicit, services.scheduled.bindInput)
	}
	if lookup.calls != 0 {
		t.Fatalf("explicit bind consulted the existing binding %d times", lookup.calls)
	}
}

func TestDMOnlyToolsAreRefusedInGroupAndAllowedInDirectMessage(t *testing.T) {
	arguments := map[string]map[string]any{
		"account_register":  {"name": "Sam", "birth_date": "1990-01-02", "consent": true},
		"identity_link":     {"code": "ABC123"},
		"invitation_create": {"space": scheduledTestSpaceID, "role_name": "space_member"},
	}
	calls := func(services groupTestServices, tool string) int {
		switch tool {
		case "account_register":
			return services.registrar.calls
		case "identity_link":
			return services.link.calls
		default:
			return services.invitation.createCalls
		}
	}
	for tool, args := range arguments {
		t.Run(tool, func(t *testing.T) {
			server, services := newGroupTestServer(t, nil)
			refused := callScheduledTool(t, server.URL, tool, cloneArguments(args), groupIdentity("nonce-group-"+tool))
			assertScheduledToolError(t, refused, dmRequiredMessage)
			if calls(services, tool) != 0 {
				t.Fatalf("%s reached its service from a group", tool)
			}

			allowed := callScheduledTool(t, server.URL, tool, cloneArguments(args), directIdentity("nonce-dm-"+tool))
			if allowed.Result == nil || allowed.Result.IsError {
				t.Fatalf("%s failed in a DM: %+v", tool, allowed)
			}
			if calls(services, tool) != 1 {
				t.Fatalf("%s did not reach its service from a DM", tool)
			}
		})
	}
}

var sharedChatCases = []struct {
	name     string
	channel  string
	chatType string
}{
	{"whatsapp group", "whatsapp", "group"},
	{"whatsapp_cloud group", "whatsapp_cloud", "group"},
	{"telegram group", "telegram", "group"},
	{"slack channel", "slack", "channel"},
	{"discord thread", "discord", "thread"},
	{"discord guild", "discord", "guild"},
}

func TestIsVerifiedSharedChat(t *testing.T) {
	cases := append([]struct {
		name     string
		channel  string
		chatType string
	}{
		{"whatsapp dm", "whatsapp", "dm"},
		{"telegram dm", "telegram", "dm"},
		{"empty chat type", "whatsapp", ""},
	}, sharedChatCases...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.chatType != "" && tc.chatType != "dm"
			request := ExternalRequest{Provider: "hermes", ExternalID: "x", Channel: tc.channel, ChatID: groupTestChatID, ChatType: tc.chatType}
			if _, shared := isVerifiedSharedChat(withVerifiedExternalRequest(context.Background(), request)); shared != want {
				t.Fatalf("verified shared = %v, want %v", shared, want)
			}
			if _, shared := isVerifiedSharedChat(WithExternalRequest(context.Background(), request)); shared {
				t.Fatal("unverified request counted as a shared chat")
			}
		})
	}
}

func TestSharedChatsOnAnyChannelRefuseDMOnlyToolsAndBlankSpaceFallback(t *testing.T) {
	for _, tc := range sharedChatCases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &groupSpaceLookupStub{}
			server, services := newGroupTestServer(t, lookup)
			identity := func(suffix string) *IdentityEnvelope {
				return channelChatIdentity("nonce-shared-"+tc.name+suffix, tc.channel, groupTestChatID, tc.chatType)
			}

			refused := callScheduledTool(t, server.URL, "identity_link", map[string]any{"code": "ABC123"}, identity("-link"))
			assertScheduledToolError(t, refused, dmRequiredMessage)
			refused = callScheduledTool(t, server.URL, "account_register", map[string]any{"name": "Sam", "birth_date": "1990-01-02", "consent": true}, identity("-register"))
			assertScheduledToolError(t, refused, dmRequiredMessage)
			refused = callScheduledTool(t, server.URL, "invitation_create", map[string]any{"space": scheduledTestSpaceID, "role_name": "space_member"}, identity("-invite"))
			assertScheduledToolError(t, refused, dmRequiredMessage)
			if services.link.calls != 0 || services.registrar.calls != 0 || services.invitation.createCalls != 0 {
				t.Fatalf("DM-only tools reached services: link=%d register=%d invite=%d", services.link.calls, services.registrar.calls, services.invitation.createCalls)
			}

			blank := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{}, identity("-blank"))
			assertScheduledToolError(t, blank, "space is required in a group chat that is not bound to a Space")
			if services.scheduled.targetCalls != 0 || services.scheduled.targetSpace == groupTestPersonalSpaceID {
				t.Fatalf("blank selector reached service (calls=%d space=%q)", services.scheduled.targetCalls, services.scheduled.targetSpace)
			}
			if lookup.calls != 1 || lookup.channel != tc.channel || lookup.chatID != groupTestChatID {
				t.Fatalf("lookup = %+v; want one call for %s/%s", lookup, tc.channel, groupTestChatID)
			}

			bind := callScheduledTool(t, server.URL, "space_channel_bind", map[string]any{}, identity("-bind"))
			assertScheduledToolError(t, bind, "space is required to bind a group chat to a Space")
			if services.scheduled.bindCalls != 0 {
				t.Fatal("blank bind reached the service")
			}
		})
	}
}

func TestSharedChatBlankSpaceWithoutChatIDFailsClosedWithoutLookup(t *testing.T) {
	lookup := &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true}
	ctx := withGroupSpaceLookup(withVerifiedExternalRequest(context.Background(), ExternalRequest{
		Provider: "hermes", ExternalID: "x", Channel: "telegram", ChatType: "group",
	}), lookup)
	if _, err := selectSpaceActor(ctx, groupResolver(), ""); err == nil || !strings.Contains(err.Error(), "space is required") {
		t.Fatalf("selectSpaceActor error = %v, want space is required", err)
	}
	if lookup.calls != 0 {
		t.Fatalf("lookup called %d times without a chat ID", lookup.calls)
	}
}

func TestDMAndEmptyChatTypeAreNotSharedChats(t *testing.T) {
	for _, tc := range []struct{ name, channel, chatType string }{
		{"whatsapp dm", "whatsapp", "dm"},
		{"telegram dm", "telegram", "dm"},
		{"empty chat type", "whatsapp", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true}
			server, services := newGroupTestServer(t, lookup)
			response := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{}, channelChatIdentity("nonce-notshared-"+tc.name, tc.channel, "6285333320090@s.whatsapp.net", tc.chatType))
			if response.Result == nil || response.Result.IsError {
				t.Fatalf("blank selector failed: %+v", response)
			}
			if lookup.calls != 0 || services.scheduled.targetSpace != groupTestPersonalSpaceID {
				t.Fatalf("space = %q lookup calls=%d; want Personal Space without lookup", services.scheduled.targetSpace, lookup.calls)
			}
			allowed := callScheduledTool(t, server.URL, "identity_link", map[string]any{"code": "ABC123"}, channelChatIdentity("nonce-notshared-link-"+tc.name, tc.channel, "6285333320090@s.whatsapp.net", tc.chatType))
			if allowed.Result == nil || allowed.Result.IsError || services.link.calls != 1 {
				t.Fatalf("identity_link refused outside a shared chat: %+v", allowed)
			}
		})
	}
}

func TestDMRequiredErrorCode(t *testing.T) {
	ctx := withVerifiedExternalRequest(context.Background(), ExternalRequest{Provider: "hermes", ExternalID: "x", Channel: "whatsapp", ChatID: groupTestChatID, ChatType: "group"})
	var mapped *MCPError
	if err := requireDirectMessage(ctx); !errors.As(err, &mapped) || mapped.Code != "dm_required" || mapped.Message != dmRequiredMessage {
		t.Fatalf("requireDirectMessage = %v, want dm_required", err)
	}
}

func TestMembershipOutputsOmitContactDetailsAndInternalIDs(t *testing.T) {
	server, _ := newGroupTestServer(t, &groupSpaceLookupStub{spaceID: scheduledTestSpaceID, found: true})
	memberID := "00000000-0000-0000-0000-000000000302"
	for name, call := range map[string]struct {
		tool string
		args map[string]any
		path []string
	}{
		"space_list":         {"space_list", map[string]any{}, []string{"spaces"}},
		"space_get_members":  {"space_get_members", map[string]any{"space": scheduledTestSpaceID}, []string{"members"}},
		"member_update_role": {"member_update_role", map[string]any{"space": scheduledTestSpaceID, "member_id": memberID, "role": "space_viewer"}, nil},
		"member_remove":      {"member_remove", map[string]any{"space": scheduledTestSpaceID, "member_id": memberID}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			response := callScheduledTool(t, server.URL, call.tool, call.args, directIdentity("nonce-membership-"+name))
			if response.Result == nil || response.Result.IsError {
				t.Fatalf("%s failed: %+v", call.tool, response)
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(response.Result.StructuredOutput, &decoded); err != nil {
				t.Fatalf("%s output is not a JSON object: %s", call.tool, response.Result.StructuredOutput)
			}
			var item map[string]any
			if len(call.path) == 1 {
				var list []map[string]any
				if err := json.Unmarshal(decoded[call.path[0]], &list); err != nil || len(list) != 1 {
					t.Fatalf("%s list = %s, %v", call.tool, decoded[call.path[0]], err)
				}
				item = list[0]
			} else {
				if err := json.Unmarshal(response.Result.StructuredOutput, &item); err != nil {
					t.Fatal(err)
				}
			}
			for _, forbidden := range []string{"user_email", "user_id", "role_id"} {
				if _, exists := item[forbidden]; exists {
					t.Errorf("%s output exposes %q: %v", call.tool, forbidden, item)
				}
			}
			for _, required := range []string{"id", "space_id", "space_name", "space_type", "space_category", "user_name", "role_name", "status"} {
				if _, exists := item[required]; !exists {
					t.Errorf("%s output lacks %q: %v", call.tool, required, item)
				}
			}
			if strings.Contains(string(response.Result.StructuredOutput), "sam@example.com") {
				t.Errorf("%s leaked the member email: %s", call.tool, response.Result.StructuredOutput)
			}
		})
	}
}

func cloneArguments(arguments map[string]any) map[string]any {
	cloned := make(map[string]any, len(arguments))
	for key, value := range arguments {
		cloned[key] = value
	}
	return cloned
}
