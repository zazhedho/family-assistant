package interfaceactivity

import (
	"context"

	domainactivity "family-assistant/internal/domain/activity"
)

type RepoActivityInterface interface {
	Create(context.Context, *domainactivity.Activity) error
	List(context.Context, domainactivity.ListFilter) ([]domainactivity.Activity, error)
}
