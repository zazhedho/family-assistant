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

// AccountUpdateInput renames and/or archives an account. Archived true sets
// archived_at; unarchiving is not supported.
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
