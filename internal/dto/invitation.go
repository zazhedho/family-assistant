package dto

import domainspace "family-assistant/internal/domain/space"

type InvitationCreateInput struct {
	SpaceID      string
	InvitedEmail string
	RoleName     string
}

// MemberAddByPhoneInput asks to add an already-registered account to a shared
// Space by verified phone. An empty ConfirmName only previews the target.
type MemberAddByPhoneInput struct {
	SpaceID     string
	Phone       string
	RoleName    string
	ConfirmName string
}

// MemberAddPreview is everything revealed about the target account: its display
// name and only the last 3 digits of its phone number.
type MemberAddPreview struct {
	DisplayName string
	SpaceName   string
	RoleName    string
	PhoneTail   string
}

type MemberAddResult struct {
	Added   bool
	Preview MemberAddPreview
	Member  *domainspace.Member
}
