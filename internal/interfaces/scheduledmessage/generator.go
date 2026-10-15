package interfacescheduledmessage

import (
	"context"

	"family-assistant/internal/dto"
)

type Generator interface {
	Generate(context.Context, dto.ScheduledMessageGenerationInput) (string, error)
}
