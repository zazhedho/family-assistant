package interfacescheduledmessage

import (
	"context"
	"time"
)

type Scheduler interface {
	Run(context.Context)
	RunOnce(context.Context, time.Time) error
}
