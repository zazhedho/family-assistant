package interfacenotification

import (
	"context"

	domainreminder "family-assistant/internal/domain/reminder"
)

type NotificationSender interface {
	Send(context.Context, domainreminder.DeliveryTarget, string) error
}
