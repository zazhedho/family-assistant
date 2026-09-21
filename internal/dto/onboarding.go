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
