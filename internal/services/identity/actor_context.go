package serviceidentity

type ActorContext struct {
	UserID            string
	MemberID          string
	FamilyID          string
	RoleID            string
	RoleName          string
	Permissions       map[string]struct{}
	HermesProfileID   string
	Channel           string
	InitiatorUserID   string
	InitiatorRoleName string
}

func (a ActorContext) HasPermission(permission string) bool {
	_, ok := a.Permissions[permission]
	return ok
}
