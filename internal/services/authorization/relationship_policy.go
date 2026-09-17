package authorization

import serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"

func allowsPersonalAccess(actor serviceidentity.ActorContext, resource Resource) bool {
	if actor.MemberID != "" && actor.MemberID == resource.OwnerMemberID {
		return true
	}

	return actor.RoleName == "parent" &&
		resource.OwnerRoleName == "child" &&
		actor.FamilyID != "" &&
		actor.FamilyID == resource.FamilyID
}
