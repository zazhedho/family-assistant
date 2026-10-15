package scheduledmessage

func CanCancel(target TargetKind, role, actorMemberID, creatorMemberID string) bool {
	if actorMemberID == "" {
		return false
	}
	if target != TargetDM && target != TargetGroup {
		return false
	}
	if isSpaceOwnerOrAdmin(role) {
		return true
	}
	return target == TargetDM && creatorMemberID != "" && actorMemberID == creatorMemberID
}

func CanUpdate(target TargetKind, role, actorMemberID, creatorMemberID string) bool {
	if actorMemberID == "" || (target != TargetDM && target != TargetGroup) {
		return false
	}
	if isSpaceOwnerOrAdmin(role) {
		return true
	}
	return creatorMemberID != "" && actorMemberID == creatorMemberID
}

func CanUnsubscribe(target TargetKind, actorMemberID, recipientMemberID string) bool {
	return target == TargetDM && actorMemberID != "" && actorMemberID == recipientMemberID
}

func CanRead(target TargetKind, role, actorMemberID, creatorMemberID, recipientMemberID string) bool {
	if actorMemberID == "" {
		return false
	}
	switch target {
	case TargetDM:
		return isSpaceOwnerOrAdmin(role) || actorMemberID == creatorMemberID || actorMemberID == recipientMemberID
	case TargetGroup:
		switch role {
		case "space_owner", "space_admin", "space_member", "space_viewer":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func isSpaceOwnerOrAdmin(role string) bool {
	return role == "space_owner" || role == "space_admin"
}
