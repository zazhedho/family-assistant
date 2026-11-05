package domainactivity

func init() { registerKind("medication", validateMedication) }

func validateMedication(d Details) error { return requireString(d, "name") }
