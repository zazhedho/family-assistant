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
	"gorm.io/gorm"
)

const (
	memberAuditResource = "space_member"
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
// Accepted exposure: an owner/admin can learn whether a verified account exists
// for a number, and its display name, only via the preview. Unknown and
// unverified numbers return the same not-found error.
func (s *service) AddMemberByPhone(ctx context.Context, actorUserID string, input dto.MemberAddByPhoneInput) (*dto.MemberAddResult, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	spaceID := strings.TrimSpace(input.SpaceID)
	roleName := utils.NormalizeKey(input.RoleName)
	fail := func(memberID, targetUserID string, err error) (*dto.MemberAddResult, error) {
		s.writeMemberAddFailure(ctx, spaceID, memberID, actorUserID, roleName, targetUserID, err)
		return nil, err
	}

	actor, err := s.invitationActor(ctx, actorUserID, spaceID, createPermission)
	if err != nil {
		return fail("", "", err)
	}
	if !validTargetRole(roleName) {
		return fail(actor.ID, "", &serviceauthorization.ValidationError{Field: "role_name", Reason: "must be one of space_admin, space_member, space_viewer"})
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
	event := s.newAuditEvent(ctx, auditEventInput{
		action:     domainaudit.ActionCreate,
		resource:   memberAuditResource,
		resourceID: member.ID,
		spaceID:    spaceID,
		memberID:   actor.ID,
		userID:     actorUserID,
		roleName:   roleName,
		targetUser: target.ID,
	})
	event.Status = domainaudit.StatusSuccess
	event.Message = "Added space member by phone"
	s.writeAudit(ctx, event)
	result.Added, result.Member = true, member
	return result, nil
}

func (s *service) writeMemberAddFailure(ctx context.Context, spaceID, memberID, actorUserID, roleName, targetUserID string, err error) {
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
