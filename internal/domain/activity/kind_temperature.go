package domainactivity

func init() { registerKind("temperature", validateTemperature) }

func validateTemperature(d Details) error {
	v, ok := d["celsius"]
	if !ok {
		return detailsErr("details.celsius", "is required")
	}
	f, ok := toFloat(v)
	if !ok || f < 30 || f > 45 {
		return detailsErr("details.celsius", "must be a number between 30 and 45")
	}
	return nil
}
