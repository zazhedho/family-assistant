package dto

type AccountRegistrationInput struct {
	Name       string
	BirthDate  string
	Consent    bool
	Provider   string
	ExternalID string
	Channel    string
}

type AccountRegistrationResult struct {
	Status  string
	UserID  string
	SpaceID string
}

type AccountLinkInput struct {
	Provider   string
	ExternalID string
	Channel    string
}

type AccountRenameInput struct {
	Name       string
	Provider   string
	ExternalID string
	Channel    string
}

type AccountRenameResult struct {
	Status string
	Name   string
}
