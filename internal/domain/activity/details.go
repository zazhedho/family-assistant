package domainactivity

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// MaxDetailsJSONBytes is the Go-side cap on compact JSON size. The database
// allows 4096 chars of jsonb text, which renders larger than compact JSON.
const MaxDetailsJSONBytes = 2700

const (
	OutcomeFinished = "finished"
	OutcomeExpired  = "expired"
)

// Details is the structured, kind-specific payload of an activity.
type Details map[string]any

// Scan implements sql.Scanner. NULL and empty values become an empty map.
func (d *Details) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("details: unsupported scan type %T", src)
	}
	if len(raw) == 0 {
		*d = Details{}
		return nil
	}
	out := Details{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("details: %w", err)
	}
	if out == nil {
		out = Details{}
	}
	*d = out
	return nil
}

// Value implements driver.Valuer. A nil map is stored as an empty object.
func (d Details) Value() (driver.Value, error) {
	if d == nil {
		return "{}", nil
	}
	b, err := json.Marshal(map[string]any(d))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// DetailsError is a validation failure for a details field.
type DetailsError struct {
	Field  string
	Reason string
}

func (e *DetailsError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

func detailsErr(field, reason string) error {
	return &DetailsError{Field: field, Reason: reason}
}

var kindAliases = map[string]string{
	"bathing":        "bath",
	"bottle_feeding": "feeding",
}

// NormalizeKind lowercases, trims and canonicalizes an activity kind.
func NormalizeKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	k = strings.Join(strings.FieldsFunc(k, func(r rune) bool { return r == ' ' || r == '-' || r == '\t' }), "_")
	if a, ok := kindAliases[k]; ok {
		return a
	}
	return k
}

// ApplyDetailsPatch merges patch into details without mutating either. A nil
// patch value removes the key.
func ApplyDetailsPatch(details, patch Details) Details {
	out := make(Details, len(details)+len(patch))
	for k, v := range details {
		out[k] = v
	}
	for k, v := range patch {
		if v == nil {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

// ValidateDetails checks details for the given kind. Unknown kinds accept any
// object. It returns *DetailsError on failure.
func ValidateDetails(kind string, details Details) error {
	b, err := json.Marshal(map[string]any(details))
	if err != nil {
		return detailsErr("details", "must be valid JSON")
	}
	if len(b) > MaxDetailsJSONBytes {
		return detailsErr("details", fmt.Sprintf("too large (max %d bytes)", MaxDetailsJSONBytes))
	}
	if v, ok := kindValidators[NormalizeKind(kind)]; ok {
		return v(details)
	}
	return nil
}

// KindValidator checks the details of one activity kind. It returns
// *DetailsError on failure.
type KindValidator func(Details) error

// kindValidators holds the per-kind rules; kinds without an entry accept any
// object. Each kind_*.go file registers itself from init().
var kindValidators = map[string]KindValidator{}

// registerKind adds the validator for a canonical kind. It panics on an empty
// or duplicate kind, since both are programming errors.
func registerKind(kind string, v KindValidator) {
	if kind == "" {
		panic("domainactivity: registerKind with empty kind")
	}
	if _, dup := kindValidators[kind]; dup {
		panic("domainactivity: duplicate kind registration: " + kind)
	}
	kindValidators[kind] = v
}

func requireString(details Details, key string) error {
	s, ok := details[key].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return detailsErr("details."+key, "is required")
	}
	return nil
}

// optInt reads an optional integer key with a minimum value.
func optInt(d Details, key string, min int) (val int, present bool, err error) {
	v, ok := d[key]
	if !ok {
		return 0, false, nil
	}
	n, ok := toInt(v)
	if !ok || n < min {
		if min > 0 {
			return 0, true, detailsErr("details."+key, "must be a positive integer")
		}
		return 0, true, detailsErr("details."+key, "must be an integer >= 0")
	}
	return n, true, nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// toInt accepts int types and whole-number float64/json.Number values.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32, float64, json.Number:
		f, ok := toFloat(n)
		if !ok || f != math.Trunc(f) || math.Abs(f) > 1e9 {
			return 0, false
		}
		return int(f), true
	}
	return 0, false
}
