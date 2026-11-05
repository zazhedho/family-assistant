package domainactivity

func init() { registerKind("growth_measurement", validateGrowthMeasurement) }

func validateGrowthMeasurement(d Details) error {
	for _, key := range []string{"weight_kg", "height_cm", "head_cm"} {
		v, ok := d[key]
		if !ok {
			continue
		}
		if f, ok := toFloat(v); !ok || f <= 0 {
			return detailsErr("details."+key, "must be a positive number")
		}
	}
	return nil
}
