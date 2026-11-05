package domainactivity

func init() { registerKind("vaccination", validateVaccination) }

func validateVaccination(d Details) error { return requireString(d, "vaccine") }
