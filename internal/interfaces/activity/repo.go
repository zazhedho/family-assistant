package interfaceactivity

import (
	"context"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
)

type RepoActivityInterface interface {
	Create(context.Context, *domainactivity.Activity) error
	List(context.Context, domainactivity.ListFilter) ([]domainactivity.Activity, error)
	FindByIDInSpace(context.Context, string, string) (*domainactivity.Activity, error)
	Update(context.Context, string, string, domainactivity.UpdateFields, time.Time) error
	SoftDelete(context.Context, string, string, time.Time) error
}
