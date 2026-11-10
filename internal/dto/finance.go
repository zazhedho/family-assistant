package dto

type AccountCreateInput struct {
	Space          string
	Name           string
	Type           string
	OpeningBalance int64
}

type AccountListInput struct {
	Space           string
	IncludeArchived bool
	Limit           int
}

// AccountUpdateInput renames, archives (Archived true) or unarchives
// (Archived false) an account; nil leaves the archive state unchanged.
type AccountUpdateInput struct {
	Space    string
	ID       string
	Name     *string
	Archived *bool
}

type CategoryCreateInput struct {
	Space string
	Name  string
	Kind  string
}

type CategoryListInput struct {
	Space           string
	Kind            string
	IncludeArchived bool
	Limit           int
}

// CategoryUpdateInput renames and/or archives a category.
type CategoryUpdateInput struct {
	Space    string
	ID       string
	Name     *string
	Archived *bool
}
