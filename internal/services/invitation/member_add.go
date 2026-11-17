package serviceinvitation

import (
	"context"
	"errors"
	"strings"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	memberAuditResource = "space_member"
	memberPreviewAction = "member_preview"
	minMemberPhoneLen   = 9
	maxMemberPhoneLen   = 15
	phoneTailLen        = 3
)

var memberPhoneSeparators = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")

// normalizeMemberPhone strips spaces, dashes, dots, parentheses and one leading
// "+", rejects any remaining non-ASCII-digit character, applies
// utils.NormalizePhoneTo62 and enforces 9..15 digits (like onboarding's
// whatsappPhone). A number without a leading 0 or 62 is kept as typed.
func normalizeMemberPhone(raw string) (string, error) {
	invalid := &serviceauthorization.ValidationError{Field: "phone", Reason: "must be a valid phone number"}
	digits := strings.TrimPrefix(memberPhoneSeparators.Replace(strings.TrimSpace(raw)), "+")
	if digits == "" {
		return "", invalid
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return "", invalid
		}
	}
	phone := utils.NormalizePhoneTo62(digits)
	if len(phone) < minMemberPhoneLen || len(phone) > maxMemberPhoneLen {
		return "", invalid
	}
	return phone, nil
}

// AddMemberByPhone adds an already-registered account to a shared Space. The
// two-step confirmation is enforced here: without confirm_name it only returns a
// preview (no write, no audit); with a confirm_name that does not equal the
// account's display name (trimmed, case-insensitive) it fails on confirm_name.
//
// Accepted exposure: any registered user who owns a shared Space can resolve a
// verified phone number to its account's display name through the preview (and
// learn the last 3 digits only in the output). This is accepted for a household
// deployment gated by the Hermes allowlist. Every successful preview is audited
// (ids only: actor, space, target user, role; never the phone or the name) so such
// lookups are detectable. Unknown and unverified numbers return the same
// not-found error. A PERSONAL or inactive Space is reported as not found.
//
// Duplicate detection follows the unique index on space_members, exactly as in
// Accept: an existing member (any status) conflicts, while a soft-deleted former
// member gets a fresh row.
func (s *service) AddMemberByPhone(ctx context.Context, actorUserID string, input dto.MemberAddByPhoneInput) (*dto.MemberAddResult, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	spaceID := strings.TrimSpace(input.SpaceID)
	roleName := utils.NormalizeKey(input.RoleName)
	fail := func(memberID, targetUserID string, err error) (*dto.MemberAddResult, error) {
		s.writeMemberAddFailure(ctx, spaceID, memberID, actorUserID, roleName, targetUserID, err)
		return nil, err
	}

	actor, err := s.invitationActorWith(ctx, actorUserID, spaceID, createPermission, ErrNotFound)
	if err != nil {
		return fail("", "", err)
	}
	if !validTargetRole(roleName) {
		return fail(actor.ID, "", &serviceauthorization.ValidationError{Field: "role_name", Reason: invalidTargetRoleReason})
	}
	phone, err := normalizeMemberPhone(input.Phone)
	if err != nil {
		return fail(actor.ID, "", err)
	}
	if s.roles == nil || s.invitations == nil {
		return fail(actor.ID, "", errors.New("member add dependencies are not configured"))
	}
	role, err := s.roles.GetByName(ctx, roleName)
	if err != nil {
		return fail(actor.ID, "", err)
	}
	if strings.TrimSpace(role.Id) == "" {
		return fail(actor.ID, "", errors.New("member role is not configured"))
	}
	target, err := s.invitations.FindUserByVerifiedPhone(ctx, phone)
	if errors.Is(err, domainidentity.ErrIdentityNotFound) || (err == nil && target == nil) {
		return fail(actor.ID, "", ErrNotFound)
	}
	if err != nil {
		return fail(actor.ID, "", err)
	}

	result := &dto.MemberAddResult{Preview: dto.MemberAddPreview{
		DisplayName: strings.TrimSpace(target.Name),
		SpaceName:   actor.SpaceName,
		RoleName:    roleName,
		PhoneTail:   phone[len(phone)-phoneTailLen:],
	}}
	confirm := strings.TrimSpace(input.ConfirmName)
	if confirm == "" {
		s.writeMemberAudit(ctx, memberPreviewAction, "", spaceID, actor.ID, actorUserID, roleName, target.ID, "Previewed member by phone")
		return result, nil
	}
	if result.Preview.DisplayName == "" || !strings.EqualFold(confirm, result.Preview.DisplayName) {
		return fail(actor.ID, target.ID, &serviceauthorization.ValidationError{Field: "confirm_name", Reason: "does not match the account name"})
	}

	member, err := s.invitations.AddMember(ctx, spaceID, target.ID, role.Id, s.now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	if err != nil {
		return fail(actor.ID, target.ID, err)
	}
	s.writeMemberAudit(ctx, domainaudit.ActionCreate, member.ID, spaceID, actor.ID, actorUserID, roleName, target.ID, "Added space member by phone")
	result.Added, result.Member = true, member
	return result, nil
}

func (s *service) writeMemberAudit(ctx context.Context, action, resourceID, spaceID, memberID, actorUserID, roleName, targetUserID, message string) {
	event := s.newAuditEvent(ctx, auditEventInput{
		action:     action,
		resource:   memberAuditResource,
		resourceID: resourceID,
		spaceID:    spaceID,
		memberID:   memberID,
		userID:     actorUserID,
		roleName:   roleName,
		targetUser: targetUserID,
	})
	event.Status = domainaudit.StatusSuccess
	event.Message = message
	s.writeAudit(ctx, event)
}

// writeMemberAddFailure never echoes caller-controlled strings: the role is
// recorded only if it is an allowed target role and the space only if it is a UUID.
func (s *service) writeMemberAddFailure(ctx context.Context, spaceID, memberID, actorUserID, roleName, targetUserID string, err error) {
	if !validTargetRole(roleName) {
		roleName = ""
	}
	if _, parseErr := uuid.Parse(spaceID); parseErr != nil {
		spaceID = ""
	}
	event := s.newAuditEvent(ctx, auditEventInput{
		action:     domainaudit.ActionCreate,
		resource:   memberAuditResource,
		spaceID:    spaceID,
		memberID:   memberID,
		userID:     actorUserID,
		roleName:   roleName,
		targetUser: targetUserID,
	})
	event.Status = domainaudit.StatusFailed
	event.ErrorMessage = FailureCategory(err)
	s.writeAudit(ctx, event)
}
