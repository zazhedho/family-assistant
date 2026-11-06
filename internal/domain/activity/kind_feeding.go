package domainactivity

import "time"

// MaxFeedingML caps every feeding ml value (volume, leftover, expired).
const MaxFeedingML = 100000

func init() { registerKind("feeding", validateFeeding) }

func validateFeeding(d Details) error {
	volume, hasVolume, err := optInt(d, "volume_ml", 1)
	if err != nil {
		return err
	}
	leftover, hasLeftover, err := optInt(d, "leftover_ml", 0)
	if err != nil {
		return err
	}
	expired, hasExpired, err := optInt(d, "expired_ml", 0)
	if err != nil {
		return err
	}
	if v, ok := d["leftover_reported_at"]; ok {
		s, isStr := v.(string)
		if !isStr {
			return detailsErr("details.leftover_reported_at", "must be an RFC3339 timestamp")
		}
		if _, perr := time.Parse(time.RFC3339, s); perr != nil {
			return detailsErr("details.leftover_reported_at", "must be an RFC3339 timestamp")
		}
	}
	outcome := ""
	if v, ok := d["outcome"]; ok {
		s, isStr := v.(string)
		if !isStr || (s != OutcomeFinished && s != OutcomeExpired) {
			return detailsErr("details.outcome", "must be finished or expired")
		}
		outcome = s
	}
	if hasVolume && hasLeftover && leftover > volume {
		return detailsErr("details.leftover_ml", "must not exceed volume_ml")
	}
	if hasVolume && hasExpired && expired > volume {
		return detailsErr("details.expired_ml", "must not exceed volume_ml")
	}
	if hasLeftover && hasExpired && expired > leftover {
		return detailsErr("details.expired_ml", "must not exceed leftover_ml")
	}
	if outcome == OutcomeExpired && !hasExpired {
		return detailsErr("details.expired_ml", "is required when outcome is expired")
	}
	return nil
}
