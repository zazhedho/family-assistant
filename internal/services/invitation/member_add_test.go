package serviceinvitation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainpermission "family-assistant/internal/domain/permission"
	domainrole "family-assistant/internal/domain/role"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/pkg/config"
	"gorm.io/gorm"
)

const (
	memberAddPhone    = "6289561002763"
	memberAddTargetID = "user-target"
	memberAddName     = "Siti Nurhaliza"
)

func memberAddService(repo *invitationRepositoryStub, membership *domainspace.ResolvedMembership, lookupErr error, audit *invitationAuditStub) interfaceinvitation.ServiceInvitationInterface {
	return NewService(
		repo,
		&invitationSpaceRepositoryStub{membership: membership, err: lookupErr},
		&invitationRoleRepositoryStub{roles: map[string]domainrole.Role{
			"space_admin":  {Id: "role-admin", Name: "space_admin"},
			"space_member": {Id: "role-member", Name: "space_member"},
			"space_viewer": {Id: "role-viewer", Name: "space_viewer"},
			"space_owner":  {Id: "role-owner", Name: "space_owner"},
		}},
		&invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{
			"role-owner":  {{Resource: "invitations", Action: "create"}},
			"role-viewer": {{Resource: "spaces", Action: "view"}},
		}},
		audit,
		config.InvitationConfig{TTL: time.Hour},
	)
}

func ownerMembership() *domainspace.ResolvedMembership {
	return &domainspace.ResolvedMembership{ID: "member-owner", SpaceID: "space-1", SpaceName: "Keluarga", SpaceType: domainspace.TypeShared, RoleID: "role-owner", Status: domainspace.StatusActive}
}

func targetRepo() *invitationRepositoryStub {
	return &invitationRepositoryStub{foundUser: &domaininvitation.UserRef{ID: memberAddTargetID, Name: memberAddName}}
}

func memberAddInput(confirm string) dto.MemberAddByPhoneInput {
	return dto.MemberAddByPhoneInput{SpaceID: "space-1", Phone: "0895-6100-2763", RoleName: "space_member", ConfirmName: confirm}
}

func TestNormalizeMemberPhone(t *testing.T) {
	valid := map[string]string{
		"0895-6100-2763":     "6289561002763",
		"+62 895 6100 2763":  "6289561002763",
		"6289561002763":      "6289561002763",
		"(0895) 6100.2763":   "6289561002763",
		"  0895 6100 2763  ": "6289561002763",
		"895610027763":       "895610027763",
		"081234567":          "6281234567",
	}
	for in, want := range valid {
		got, err := normalizeMemberPhone(in)
		if err != nil || got != want {
			t.Errorf("normalizeMemberPhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	invalid := []string{"", "   ", "+", "0895abc2763", "08956100x2763", "62895+6100", "++62895610027", "٠٨٩٥٦١٠٠٢٧٦٣", "1234567", "081234567890123456", "62-895-6100-2763-99999"}
	for _, in := range invalid {
		if got, err := normalizeMemberPhone(in); err == nil {
			t.Errorf("normalizeMemberPhone(%q) = %q, want error", in, got)
		}
	}
}

func TestMemberAddPreviewHasNoWrite(t *testing.T) {
	repo, audit := targetRepo(), &invitationAuditStub{}
	svc := memberAddService(repo, ownerMembership(), nil, audit)
	for _, confirm := range []string{"", "   "} {
		res, err := svc.AddMemberByPhone(context.Background(), "user-owner", memberAddInput(confirm))
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		if res.Added || res.Member != nil {
			t.Fatalf("preview added a member: %+v", res)
		}
		want := dto.MemberAddPreview{DisplayName: memberAddName, SpaceName: "Keluarga", RoleName: "space_member", PhoneTail: "763"}
		if res.Preview != want {
			t.Fatalf("preview = %+v, want %+v", res.Preview, want)
		}
	}
	if repo.addCalls != 0 {
		t.Fatalf("preview wrote a membership: addCalls=%d", repo.addCalls)
	}
	if len(audit.events) != 2 {
		t.Fatalf("want one preview audit per preview, got %d", len(audit.events))
	}
	if repo.findUserPhone != memberAddPhone {
		t.Fatalf("lookup phone = %q", repo.findUserPhone)
	}
}

func TestMemberAddWithMatchingNameAddsOnceAndAuditsIDsOnly(t *testing.T) {
	for _, confirm := range []string{memberAddName, "  siti NURHALIZA \n"} {
		repo, audit := targetRepo(), &invitationAuditStub{}
		svc := memberAddService(repo, ownerMembership(), nil, audit)
		res, err := svc.AddMemberByPhone(context.Background(), "user-owner", memberAddInput(confirm))
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		if !res.Added || res.Member == nil || res.Member.UserID != memberAddTargetID {
			t.Fatalf("result = %+v", res)
		}
		if repo.addCalls != 1 || repo.addSpaceID != "space-1" || repo.addUserID != memberAddTargetID || repo.addRoleID != "role-member" {
			t.Fatalf("add call = %d %q %q %q", repo.addCalls, repo.addSpaceID, repo.addUserID, repo.addRoleID)
		}
		if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusSuccess {
			t.Fatalf("audit = %+v", audit.events)
		}
		encoded, _ := json.Marshal(audit.events[0])
		for _, secret := range []string{"2763", "6100", memberAddName, "Siti", "Nurhaliza"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("audit leaked %q: %s", secret, encoded)
			}
		}
		for _, id := range []string{"space-1", "member-owner", memberAddTargetID, "space_member"} {
			if !strings.Contains(string(encoded), id) {
				t.Fatalf("audit missing id %q: %s", id, encoded)
			}
		}
	}
}

func TestMemberAddNameMismatchIsValidationErrorOnConfirmName(t *testing.T) {
	for _, confirm := range []string{"Someone Else", "Siti", "Siti Nurhaliza Extra", "S1ti Nurhaliza"} {
		repo, audit := targetRepo(), &invitationAuditStub{}
		svc := memberAddService(repo, ownerMembership(), nil, audit)
		_, err := svc.AddMemberByPhone(context.Background(), "user-owner", memberAddInput(confirm))
		var validationErr *serviceauthorization.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "confirm_name" {
			t.Fatalf("confirm %q: error = %v, want confirm_name validation", confirm, err)
		}
		if repo.addCalls != 0 {
			t.Fatalf("confirm %q: member written", confirm)
		}
		if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != "validation" {
			t.Fatalf("audit = %+v", audit.events)
		}
		encoded, _ := json.Marshal(audit.events[0])
		if strings.Contains(string(encoded), "Siti") || strings.Contains(string(encoded), "2763") {
			t.Fatalf("failure audit leaked: %s", encoded)
		}
	}
}

func TestMemberAddUnknownPhoneIsNotFoundAndWritesNothing(t *testing.T) {
	repo, audit := &invitationRepositoryStub{findUserErr: domainidentity.ErrIdentityNotFound}, &invitationAuditStub{}
	svc := memberAddService(repo, ownerMembership(), nil, audit)
	_, err := svc.AddMemberByPhone(context.Background(), "user-owner", memberAddInput(memberAddName))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
	if repo.addCalls != 0 || len(audit.events) != 1 || audit.events[0].ErrorMessage != "not_found" {
		t.Fatalf("addCalls=%d audit=%+v", repo.addCalls, audit.events)
	}
}

func TestMemberAddAuthorization(t *testing.T) {
	viewer := &domainspace.ResolvedMembership{ID: "member-v", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-viewer", Status: domainspace.StatusActive}
	personal := ownerMembership()
	personal.SpaceType = domainspace.TypePersonal
	otherSpace := ownerMembership()
	otherSpace.SpaceID = "space-other"
	cases := []struct {
		name       string
		membership *domainspace.ResolvedMembership
		lookupErr  error
		want       error
	}{
		{"viewer or member without permission", viewer, nil, ErrForbidden},
		{"personal space", personal, nil, ErrNotFound},
		{"non member (nil)", nil, nil, ErrNotFound},
		{"non member (record not found)", nil, gorm.ErrRecordNotFound, ErrNotFound},
		{"member of another space", otherSpace, nil, ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, audit := targetRepo(), &invitationAuditStub{}
			svc := memberAddService(repo, tc.membership, tc.lookupErr, audit)
			for _, confirm := range []string{"", memberAddName} {
				_, err := svc.AddMemberByPhone(context.Background(), "user-x", memberAddInput(confirm))
				if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
			}
			if repo.addCalls != 0 || repo.findUserCalls != 0 {
				t.Fatalf("unauthorized actor reached repo: add=%d find=%d", repo.addCalls, repo.findUserCalls)
			}
			if len(audit.events) != 2 {
				t.Fatalf("want a failed audit per rejection, got %d", len(audit.events))
			}
		})
	}
}

func TestMemberAddRejectsOwnerAndUnknownRoles(t *testing.T) {
	for _, role := range []string{"space_owner", "owner", "", "parent", "SPACE_OWNER"} {
		repo, audit := targetRepo(), &invitationAuditStub{}
		svc := memberAddService(repo, ownerMembership(), nil, audit)
		in := memberAddInput(memberAddName)
		in.RoleName = role
		_, err := svc.AddMemberByPhone(context.Background(), "user-owner", in)
		var validationErr *serviceauthorization.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "role_name" {
			t.Fatalf("role %q: error = %v", role, err)
		}
		if repo.addCalls != 0 || repo.findUserCalls != 0 {
			t.Fatalf("role %q reached repo", role)
		}
	}
	for _, role := range []string{"space_admin", "space_member", "space_viewer"} {
		repo := targetRepo()
		in := memberAddInput(memberAddName)
		in.RoleName = role
		if _, err := memberAddService(repo, ownerMembership(), nil, &invitationAuditStub{}).AddMemberByPhone(context.Background(), "user-owner", in); err != nil {
			t.Fatalf("role %q: %v", role, err)
		}
	}
}

func TestMemberAddInvalidPhoneIsValidationErrorBeforeLookup(t *testing.T) {
	repo, audit := targetRepo(), &invitationAuditStub{}
	svc := memberAddService(repo, ownerMembership(), nil, audit)
	in := memberAddInput(memberAddName)
	in.Phone = "08x5"
	_, err := svc.AddMemberByPhone(context.Background(), "user-owner", in)
	var validationErr *serviceauthorization.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "phone" {
		t.Fatalf("error = %v", err)
	}
	if repo.findUserCalls != 0 || repo.addCalls != 0 || len(audit.events) != 1 {
		t.Fatalf("find=%d add=%d audit=%d", repo.findUserCalls, repo.addCalls, len(audit.events))
	}
	encoded, _ := json.Marshal(audit.events[0])
	if strings.Contains(string(encoded), "08x5") {
		t.Fatalf("audit leaked phone: %s", encoded)
	}
}

func TestMemberAddDuplicateMembershipIsConflict(t *testing.T) {
	repo, audit := targetRepo(), &invitationAuditStub{}
	repo.addMemberErr = ErrMembershipConflict
	svc := memberAddService(repo, ownerMembership(), nil, audit)
	_, err := svc.AddMemberByPhone(context.Background(), "user-owner", memberAddInput(memberAddName))
	if !errors.Is(err, ErrMembershipConflict) {
		t.Fatalf("error = %v, want conflict", err)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed {
		t.Fatalf("audit = %+v", audit.events)
	}
}

func TestMemberAddSpaceVanishedMapsToNotFound(t *testing.T) {
	repo := targetRepo()
	repo.addMemberErr = gorm.ErrRecordNotFound
	_, err := memberAddService(repo, ownerMembership(), nil, &invitationAuditStub{}).AddMemberByPhone(context.Background(), "user-owner", memberAddInput(memberAddName))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
}

func TestMemberAddPreviewAuditsIDsOnly(t *testing.T) {
	repo, audit := targetRepo(), &invitationAuditStub{}
	svc := memberAddService(repo, ownerMembership(), nil, audit)
	if _, err := svc.AddMemberByPhone(context.Background(), "user-owner", memberAddInput("")); err != nil {
		t.Fatalf("preview: %v", err)
	}
	if repo.addCalls != 0 || len(audit.events) != 1 {
		t.Fatalf("addCalls=%d audit=%d", repo.addCalls, len(audit.events))
	}
	event := audit.events[0]
	if event.Action != "member_preview" || event.Status != domainaudit.StatusSuccess || event.Resource != "space_member" || event.ActorMemberID != "member-owner" || event.ActorUserID != "user-owner" {
		t.Fatalf("event = %+v", event)
	}
	if event.Metadata["space_id"] != "space-1" || event.Metadata["target_user_id"] != memberAddTargetID || event.Metadata["role"] != "space_member" {
		t.Fatalf("metadata = %+v", event.Metadata)
	}
	encoded, _ := json.Marshal(event)
	for _, secret := range []string{"2763", "6100", "0895", memberAddPhone, memberAddName, "Siti", "Nurhaliza"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("preview audit leaked %q: %s", secret, encoded)
		}
	}
}

func TestMemberAddPersonalSpaceReturnsSameNotFoundAsMissing(t *testing.T) {
	personal := ownerMembership()
	personal.SpaceType = domainspace.TypePersonal
	audit := &invitationAuditStub{}
	_, err := memberAddService(targetRepo(), personal, nil, audit).AddMemberByPhone(context.Background(), "user-owner", memberAddInput(memberAddName))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want exactly ErrNotFound", err)
	}
	if len(audit.events) != 1 || audit.events[0].ErrorMessage != "not_found" {
		t.Fatalf("audit = %+v", audit.events)
	}
}

func TestMemberAddFailureAuditCategoryOnForbiddenAndNotFound(t *testing.T) {
	member := &domainspace.ResolvedMembership{ID: "member-m", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-member", Status: domainspace.StatusActive}
	for _, tc := range []struct {
		name       string
		membership *domainspace.ResolvedMembership
		want       string
	}{{"space_member role", member, "forbidden"}, {"non member", nil, "not_found"}} {
		repo, audit := targetRepo(), &invitationAuditStub{}
		svc := memberAddService(repo, tc.membership, nil, audit)
		if _, err := svc.AddMemberByPhone(context.Background(), "user-x", memberAddInput(memberAddName)); err == nil {
			t.Fatalf("%s: no error", tc.name)
		}
		if repo.findUserCalls != 0 || repo.addCalls != 0 {
			t.Fatalf("%s: reached repo", tc.name)
		}
		if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != tc.want {
			t.Fatalf("%s: audit = %+v", tc.name, audit.events)
		}
	}
}

func TestMemberAddSpaceAdminActorCanAdd(t *testing.T) {
	admin := &domainspace.ResolvedMembership{ID: "member-admin", SpaceID: "space-1", SpaceName: "Keluarga", SpaceType: domainspace.TypeShared, RoleID: "role-admin", Status: domainspace.StatusActive}
	repo := targetRepo()
	svc := NewService(
		repo,
		&invitationSpaceRepositoryStub{membership: admin},
		&invitationRoleRepositoryStub{roles: map[string]domainrole.Role{"space_member": {Id: "role-member", Name: "space_member"}}},
		&invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{"role-admin": {{Resource: "invitations", Action: "create"}}}},
		&invitationAuditStub{},
		config.InvitationConfig{TTL: time.Hour},
	)
	res, err := svc.AddMemberByPhone(context.Background(), "user-admin", memberAddInput(memberAddName))
	if err != nil || !res.Added || repo.addCalls != 1 {
		t.Fatalf("admin add: res=%+v err=%v adds=%d", res, err, repo.addCalls)
	}
}

func TestMemberAddConfirmNameNeverMatchesWeirdInputs(t *testing.T) {
	cases := []struct{ account, confirm string }{
		{"", "x"},
		{"   ", "x"},
		{memberAddName, "Siti\u200bNurhaliza"},
		{memberAddName, "Siti Nurhaliza\u200b"},
		{"Jose", "Jos\u00e9"},
		{"Jos\u00e9", "Jose"},
	}
	for _, tc := range cases {
		repo := &invitationRepositoryStub{foundUser: &domaininvitation.UserRef{ID: memberAddTargetID, Name: tc.account}}
		_, err := memberAddService(repo, ownerMembership(), nil, &invitationAuditStub{}).AddMemberByPhone(context.Background(), "user-owner", memberAddInput(tc.confirm))
		var validationErr *serviceauthorization.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "confirm_name" || repo.addCalls != 0 {
			t.Fatalf("account %q confirm %q: err=%v adds=%d", tc.account, tc.confirm, err, repo.addCalls)
		}
	}
}

func TestMemberAddFailureAuditDoesNotEchoCallerControlledStrings(t *testing.T) {
	repo, audit := targetRepo(), &invitationAuditStub{}
	svc := memberAddService(repo, nil, nil, audit)
	in := dto.MemberAddByPhoneInput{SpaceID: "evil-space-<script>", Phone: "0895", RoleName: "evil-role", ConfirmName: "x"}
	if _, err := svc.AddMemberByPhone(context.Background(), "user-x", in); err == nil {
		t.Fatal("want error")
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit = %+v", audit.events)
	}
	encoded, _ := json.Marshal(audit.events[0])
	if strings.Contains(string(encoded), "evil") {
		t.Fatalf("audit echoed caller string: %s", encoded)
	}

	// A well-formed UUID space id and a valid role are kept.
	audit2 := &invitationAuditStub{}
	svc = memberAddService(repo, nil, nil, audit2)
	in = dto.MemberAddByPhoneInput{SpaceID: "0b6f1e0e-1d4f-4f5e-8f55-2f6a0c1e9a11", Phone: "0895", RoleName: "space_member"}
	_, _ = svc.AddMemberByPhone(context.Background(), "user-x", in)
	if audit2.events[0].Metadata["space_id"] != in.SpaceID || audit2.events[0].Metadata["role"] != "space_member" {
		t.Fatalf("metadata = %+v", audit2.events[0].Metadata)
	}
}
