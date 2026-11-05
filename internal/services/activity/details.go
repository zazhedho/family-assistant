package serviceactivity

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	domainactivity "family-assistant/internal/domain/activity"
	serviceauthorization "family-assistant/internal/services/authorization"
)

const maxSubjectLength = 128

func normalizeSubject(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if utf8.RuneCountInString(subject) > maxSubjectLength {
		return "", &serviceauthorization.ValidationError{Field: "subject", Reason: "must be at most 128 characters"}
	}
	return subject, nil
}

// detailsValidationError converts a domain details error into a service
// validation error. prefix is prepended to fields that are not already
// details paths, so report errors raised on create name the details key.
func detailsValidationError(err error, prefix string) error {
	var detailsErr *domainactivity.DetailsError
	if !errors.As(err, &detailsErr) {
		return err
	}
	field := detailsErr.Field
	if prefix != "" && field != "details" && !strings.HasPrefix(field, "details.") {
		if field == "report" {
			field = "details"
		} else {
			field = prefix + field
		}
	}
	return &serviceauthorization.ValidationError{Field: field, Reason: detailsErr.Reason}
}

// createHook adjusts the details of a new activity of one kind. It must call
// validate at the point the generic validation should run, and returns the
// details to store plus a generated note ("" keeps the caller's note).
type createHook func(subject string, details domainactivity.Details, reportedAt time.Time, validate func() error) (domainactivity.Details, string, error)

// updateHook guards an update of one kind. stored is the normalized kind of
// the stored activity. It must call validate at the point the generic
// validation should run, and may set fields.Note.
type updateHook func(stored string, activity *domainactivity.Activity, fields *domainactivity.UpdateFields, merged domainactivity.Details, validate func() error) error

// Per-kind hooks, registered from init() in the kind's own file. Kinds
// without an entry get only the generic handling.
var (
	createHooks = map[string]createHook{}
	updateHooks = map[string]updateHook{}
)

// registerKindHooks registers the hooks of a kind; either may be nil. It
// panics on an empty or duplicate kind.
func registerKindHooks(kind string, create createHook, update updateHook) {
	if kind == "" {
		panic("serviceactivity: registerKindHooks with empty kind")
	}
	if _, dup := createHooks[kind]; dup {
		panic("serviceactivity: duplicate hooks for kind " + kind)
	}
	if _, dup := updateHooks[kind]; dup {
		panic("serviceactivity: duplicate hooks for kind " + kind)
	}
	if create != nil {
		createHooks[kind] = create
	}
	if update != nil {
		updateHooks[kind] = update
	}
}

// createDetails validates the details of a new activity and returns them with
// the note to store. A kind hook may generate the note and rewrite details.
func createDetails(kind, subject string, raw map[string]any, note string, reportedAt time.Time) (domainactivity.Details, string, error) {
	details := domainactivity.ApplyDetailsPatch(nil, domainactivity.Details(raw))
	validate := func() error {
		if err := domainactivity.ValidateDetails(kind, details); err != nil {
			return detailsValidationError(err, "")
		}
		return nil
	}
	if hook := createHooks[kind]; hook != nil {
		next, generated, err := hook(subject, details, reportedAt, validate)
		if err != nil {
			return nil, "", err
		}
		details = next
		if generated != "" {
			return details, generated, nil
		}
	} else if err := validate(); err != nil {
		return nil, "", err
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, "", &serviceauthorization.ValidationError{Field: "note", Reason: requiredReason}
	}
	return details, note, nil
}

// prepareDetailsUpdate validates an update against the merged details and the
// resulting kind, running that kind's update hook if it has one. It returns
// the merged details.
func prepareDetailsUpdate(activity *domainactivity.Activity, fields *domainactivity.UpdateFields) (domainactivity.Details, error) {
	stored := domainactivity.NormalizeKind(activity.Kind)
	kind := stored
	if fields.Kind != nil {
		kind = *fields.Kind
	}
	merged := domainactivity.ApplyDetailsPatch(activity.Details, fields.DetailsPatch)
	validate := func() error {
		if fields.Kind != nil || len(fields.DetailsPatch) > 0 {
			if err := domainactivity.ValidateDetails(kind, merged); err != nil {
				return detailsValidationError(err, "")
			}
		}
		return nil
	}
	if hook := updateHooks[kind]; hook != nil {
		if err := hook(stored, activity, fields, merged, validate); err != nil {
			return nil, err
		}
		return merged, nil
	}
	if err := validate(); err != nil {
		return nil, err
	}
	return merged, nil
}
