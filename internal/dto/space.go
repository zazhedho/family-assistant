package dto

type SpaceCreateInput struct {
	Name     string
	Category string
}

type SpaceUpdateInput struct {
	Name     *string
	Category *string
}

type MemberRoleUpdateInput struct {
	Role string
}
