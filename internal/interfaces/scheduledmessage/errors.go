package interfacescheduledmessage

import "errors"

var (
	ErrConflict                 = errors.New("scheduled message conflict")
	ErrClaimConflict            = errors.New("scheduled message claim conflict")
	ErrAttemptLimitReached      = errors.New("scheduled message attempt limit reached")
	ErrInvalidTarget            = errors.New("scheduled message current target is invalid")
	ErrInvalidWhatsAppIdentity  = errors.New("invalid WhatsApp identity")
	ErrDispatchOutcomeUncertain = errors.New("scheduled message dispatch outcome uncertain")
)
