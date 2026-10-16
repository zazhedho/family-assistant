package servicescheduledmessage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
	interfacenotification "family-assistant/internal/interfaces/notification"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	"family-assistant/pkg/config"
	"family-assistant/pkg/logger"
	"family-assistant/utils"
)

const (
	maxGeneratedHistory           = 5
	maximumOutputBytes            = 2000
	scheduledMessageBridgeTimeout = 10 * time.Second
	invalidTargetReason           = "invalid_target"
	generationFailure             = "generation_failed"
	deliveryFailure               = "delivery_failed"
)

func pointerStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var (
	errScheduledMessageSchedulerConfig = errors.New("scheduled message scheduler is not configured")
	errScheduledMessageState           = errors.New("scheduled message state operation failed")
	errScheduledMessageClaim           = errors.New("scheduled message claim failed")
)

type ScheduledMessageScheduler struct {
	repository interfacescheduledmessage.SchedulerRepository
	index      interfacescheduledmessage.DueIndex
	generator  interfacescheduledmessage.Generator
	sender     interfacenotification.NotificationSender
	config     config.ScheduledMessageConfig
	now        func() time.Time

	fallbackMu   sync.Mutex
	lastFallback time.Time
}

func NewScheduledMessageScheduler(
	repository interfacescheduledmessage.SchedulerRepository,
	index interfacescheduledmessage.DueIndex,
	generator interfacescheduledmessage.Generator,
	sender interfacenotification.NotificationSender,
	cfg config.ScheduledMessageConfig,
	options dto.ScheduledMessageSchedulerOptions,
) (*ScheduledMessageScheduler, error) {
	if repository == nil || sender == nil || !cfg.Enabled {
		return nil, errScheduledMessageSchedulerConfig
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ScheduledMessageScheduler{
		repository: repository, index: index, generator: generator, sender: sender,
		config: cfg, now: now,
	}, nil
}

func (s *ScheduledMessageScheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	ctx = schedulerContext(ctx)
	if err := s.reconcileIndex(ctx); err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message index reconciliation failed")
	}
	if err := s.RunOnce(ctx, s.now()); err != nil {
		logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message worker iteration failed")
	}
	ticker := time.NewTicker(s.config.Scheduler.Interval)
	reconcileTicker := time.NewTicker(s.config.Scheduler.IndexReconcileInterval)
	defer ticker.Stop()
	defer reconcileTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.RunOnce(ctx, now.UTC()); err != nil {
				logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message worker iteration failed")
			}
		case <-reconcileTicker.C:
			if err := s.reconcileIndex(ctx); err != nil {
				logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message index reconciliation failed")
			}
		}
	}
}

func (s *ScheduledMessageScheduler) RunOnce(ctx context.Context, now time.Time) error {
	if s == nil || s.repository == nil {
		return errScheduledMessageSchedulerConfig
	}
	ctx = schedulerContext(ctx)
	if now.IsZero() {
		now = s.now()
	}
	shouldClaim, err := s.shouldClaim(ctx, now)
	if err != nil || !shouldClaim {
		return err
	}
	finishIndexMutation := s.beginIndexMutation()
	claimed, err := s.repository.ClaimDue(ctx, now, s.config.Lease, s.config.BatchSize)
	if err != nil {
		finishIndexMutation(err)
		return errScheduledMessageClaim
	}
	var indexErr error
	for i := range claimed {
		indexErr = errors.Join(indexErr, s.removeFromIndex(ctx, claimed[i].ID))
	}
	finishIndexMutation(indexErr)
	failures := make(chan error, len(claimed))
	var workers sync.WaitGroup
	for i := range claimed {
		row := claimed[i]
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := s.process(ctx, &row); err != nil {
				failures <- err
			}
		}()
	}
	workers.Wait()
	close(failures)
	var workerErrors []error
	ctxErr := ctx.Err()
	for err := range failures {
		switch {
		case errors.Is(err, interfacescheduledmessage.ErrClaimConflict):
			continue
		case ctxErr != nil && errors.Is(err, ctxErr):
			continue
		default:
			workerErrors = append(workerErrors, err)
		}
	}
	return errors.Join(workerErrors...)
}

func (s *ScheduledMessageScheduler) shouldClaim(ctx context.Context, now time.Time) (bool, error) {
	if s.index == nil {
		return s.useDatabaseFallback(now), nil
	}
	hasDue, err := s.index.HasDue(ctx, now)
	if err == nil && hasDue {
		return true, nil
	}
	if s.useDatabaseFallback(now) {
		return true, nil
	}
	return false, nil
}

func (s *ScheduledMessageScheduler) useDatabaseFallback(now time.Time) bool {
	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()
	if !s.lastFallback.IsZero() && now.Sub(s.lastFallback) < s.config.Scheduler.DatabaseFallbackInterval {
		return false
	}
	s.lastFallback = now
	return true
}

func (s *ScheduledMessageScheduler) reconcileIndex(ctx context.Context) error {
	if s == nil || s.index == nil || s.repository == nil {
		return nil
	}
	version := s.index.ReconcileVersion()
	indexedIDs, err := s.index.AllIDs(ctx)
	if err != nil {
		return errScheduledMessageState
	}
	pending, err := s.repository.ListPendingForIndex(ctx)
	if err != nil {
		return errScheduledMessageState
	}
	activeIDs := make(map[string]struct{}, len(pending))
	for i := range pending {
		activeIDs[pending[i].ID] = struct{}{}
	}
	if err := s.index.UpsertMany(ctx, pending); err != nil {
		return errScheduledMessageState
	}
	staleIDs := make([]string, 0)
	for _, id := range indexedIDs {
		if _, ok := activeIDs[id]; !ok {
			staleIDs = append(staleIDs, id)
		}
	}
	if err := s.index.RemoveMany(ctx, staleIDs); err != nil {
		return errScheduledMessageState
	}
	s.index.MarkReconciled(version)
	return nil
}

func (s *ScheduledMessageScheduler) process(ctx context.Context, claimed *domainscheduledmessage.Delivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if claimed == nil || claimed.LeaseToken == nil {
		return nil
	}
	schedule, delivery, _, _, recipientName, err := s.repository.LoadClaimed(ctx, claimed.ID, *claimed.LeaseToken, s.now())
	if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
		return err
	}
	if isInvalidScheduledMessageTarget(err) {
		return s.cancelInvalidTarget(ctx, schedule, delivery)
	}
	if err != nil {
		return errScheduledMessageState
	}
	if schedule == nil || delivery == nil {
		return errScheduledMessageState
	}
	content, schedule, delivery, shouldDispatch, err := s.contentForDelivery(ctx, schedule, delivery, recipientName)
	if err != nil {
		return err
	}
	if !shouldDispatch {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	contentLimit := s.config.MaxOutputBytes
	if schedule.ContentMode == domainscheduledmessage.ContentModeFixed {
		contentLimit = s.config.MaxTextBytes
	}
	if content == "" || len(content) > contentLimit {
		return s.cancelInvalidTarget(ctx, schedule, delivery)
	}
	return s.dispatch(ctx, schedule, delivery, content)
}

func (s *ScheduledMessageScheduler) contentForDelivery(
	ctx context.Context,
	schedule *domainscheduledmessage.Schedule,
	delivery *domainscheduledmessage.Delivery,
	recipientName string,
) (string, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, bool, error) {
	content := ""
	if delivery.ContentSnapshot != nil {
		content = *delivery.ContentSnapshot
		return content, schedule, delivery, true, nil
	} else if schedule.ContentMode == domainscheduledmessage.ContentModeFixed {
		content = schedule.Content
		return content, schedule, delivery, true, nil
	} else if schedule.ContentMode == domainscheduledmessage.ContentModeAI {
		content, schedule, delivery, err := s.generateContent(ctx, schedule, delivery, recipientName)
		if err != nil || content == "" {
			return content, schedule, delivery, false, err
		}
		return content, schedule, delivery, true, nil
	}
	return content, schedule, delivery, true, nil
}

func (s *ScheduledMessageScheduler) generateContent(
	ctx context.Context,
	schedule *domainscheduledmessage.Schedule,
	delivery *domainscheduledmessage.Delivery,
	recipientName string,
) (string, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, error) {
	if s.generator == nil {
		return "", schedule, delivery, s.finishGenerationFailure(ctx, schedule, delivery, "generation_unavailable")
	}
	recent, ready, err := s.prepareGeneration(ctx, schedule, delivery)
	if err != nil || !ready {
		return "", schedule, delivery, err
	}
	input := dto.ScheduledMessageGenerationInput{
		Theme: schedule.Content, RecipientName: strings.TrimSpace(recipientName),
		ScheduledAt: delivery.ScheduledAt, Timezone: schedule.Timezone, RecentMessages: recent,
	}
	callCtx, cancel := context.WithTimeout(ctx, s.config.GeneratorTimeout)
	generated, generationErr := s.generator.Generate(callCtx, input)
	cancel()
	if err := ctx.Err(); err != nil {
		return "", schedule, delivery, err
	}
	generated = strings.TrimSpace(generated)
	if generationErr != nil || generated == "" || len(generated) > s.config.MaxOutputBytes {
		return "", schedule, delivery, s.retryOrFinish(ctx, schedule, delivery, true)
	}
	return s.persistGeneratedContent(ctx, schedule, delivery, generated)
}

func (s *ScheduledMessageScheduler) prepareGeneration(
	ctx context.Context,
	schedule *domainscheduledmessage.Schedule,
	delivery *domainscheduledmessage.Delivery,
) ([]string, bool, error) {
	recent, err := s.repository.ListGeneratedHistory(ctx, schedule.SpaceID, schedule.ID, maxGeneratedHistory)
	if err != nil {
		return nil, false, errScheduledMessageState
	}
	if len(recent) > maxGeneratedHistory {
		recent = recent[len(recent)-maxGeneratedHistory:]
	}
	if err := s.repository.ReserveGenerationAttempt(ctx, delivery, s.now(), s.config.GenerationMaxRetries+1); err != nil {
		if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
			return nil, false, err
		}
		if errors.Is(err, interfacescheduledmessage.ErrAttemptLimitReached) {
			return nil, false, s.finishGenerationFailure(ctx, schedule, delivery, generationFailure)
		}
		return nil, false, errScheduledMessageState
	}
	if !leaseCovers(delivery, s.now(), s.config.GeneratorTimeout) {
		return recent, false, nil
	}
	return recent, true, nil
}

func (s *ScheduledMessageScheduler) persistGeneratedContent(
	ctx context.Context,
	schedule *domainscheduledmessage.Schedule,
	delivery *domainscheduledmessage.Delivery,
	generated string,
) (string, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, error) {
	currentSchedule, currentDelivery, _, _, _, loadErr := s.repository.LoadClaimed(ctx, delivery.ID, pointerStringValue(delivery.LeaseToken), s.now())
	if errors.Is(loadErr, interfacescheduledmessage.ErrClaimConflict) {
		return "", schedule, delivery, loadErr
	}
	if isInvalidScheduledMessageTarget(loadErr) {
		return "", currentSchedule, currentDelivery, s.cancelInvalidTarget(ctx, currentSchedule, currentDelivery)
	}
	if loadErr != nil {
		return "", schedule, delivery, errScheduledMessageState
	}
	if err := s.repository.PersistGeneratedContent(ctx, currentDelivery, generated, s.now()); err != nil {
		if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
			return "", currentSchedule, currentDelivery, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", currentSchedule, currentDelivery, ctxErr
		}
		return "", currentSchedule, currentDelivery, errScheduledMessageState
	}
	return generated, currentSchedule, currentDelivery, nil
}

func (s *ScheduledMessageScheduler) dispatch(ctx context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery, content string) error {
	if err := s.repository.ReserveDeliveryAttempt(ctx, delivery, s.now(), s.config.DeliveryMaxRetries+1); err != nil {
		if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
			return err
		}
		if errors.Is(err, interfacescheduledmessage.ErrAttemptLimitReached) {
			return s.finishDeliveryFailure(ctx, schedule, delivery)
		}
		return errScheduledMessageState
	}
	var next *domainscheduledmessage.Delivery
	callback := func(callCtx context.Context, current *domainscheduledmessage.Schedule, currentDelivery *domainscheduledmessage.Delivery, channel *domainscheduledmessage.SpaceChannel, identity *domainidentity.ExternalIdentity) (time.Time, error) {
		if !leaseCovers(currentDelivery, s.now(), scheduledMessageBridgeTimeout) {
			return time.Time{}, interfacescheduledmessage.ErrClaimConflict
		}
		target, err := scheduledMessageTarget(channel, identity)
		if err != nil {
			return time.Time{}, err
		}
		sendCtx, cancel := context.WithTimeout(callCtx, scheduledMessageBridgeTimeout)
		err = s.sender.Send(sendCtx, target, scheduledMessageText(current, content))
		cancel()
		if err != nil {
			return time.Time{}, err
		}
		return s.now().UTC(), nil
	}
	factory := func(current *domainscheduledmessage.Schedule, acceptedAt time.Time) (*domainscheduledmessage.Delivery, error) {
		var err error
		next, err = s.nextOccurrence(current, acceptedAt)
		return next, err
	}
	finishIndexMutation := s.beginIndexMutation()
	err := s.repository.DispatchAndFinalize(ctx, delivery.ID, pointerStringValue(delivery.LeaseToken), s.now(), factory, callback)
	if errors.Is(err, interfacescheduledmessage.ErrDispatchOutcomeUncertain) {
		finishIndexMutation(err)
		return interfacescheduledmessage.ErrDispatchOutcomeUncertain
	}
	if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
		finishIndexMutation(err)
		return err
	}
	if isInvalidScheduledMessageTarget(err) {
		finishIndexMutation(err)
		return s.cancelInvalidTarget(ctx, schedule, delivery)
	}
	if err != nil {
		finishIndexMutation(err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return s.retryOrFinish(ctx, schedule, delivery, false)
	}
	var indexErr error
	if next == nil {
		indexErr = s.removeFromIndex(ctx, delivery.ID)
	} else {
		indexErr = s.upsertIndex(ctx, next.ID, next.ScheduledAt)
	}
	finishIndexMutation(indexErr)
	return nil
}

func (s *ScheduledMessageScheduler) retryOrFinish(ctx context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery, generation bool) error {
	now := s.now().UTC()
	attempts, maxRetries, backoff, category := delivery.DeliveryAttempts, s.config.DeliveryMaxRetries, s.config.DeliveryRetryBackoff, deliveryFailure
	if generation {
		attempts, maxRetries, backoff, category = delivery.GenerationAttempts, s.config.GenerationMaxRetries, s.config.GenerationRetryBackoff, generationFailure
	}
	if attempts >= maxRetries+1 {
		if generation {
			return s.finishGenerationFailureAt(ctx, schedule, delivery, category, now)
		}
		return s.finishDeliveryFailureAt(ctx, schedule, delivery, now)
	}
	nextAttempt := now.Add(backoff)
	delivery.Status = domainscheduledmessage.DeliveryStatusPending
	delivery.NextAttemptAt = &nextAttempt
	delivery.FailureCategory = &category
	delivery.UpdatedAt = now
	schedule.UpdatedAt = now
	finishIndexMutation := s.beginIndexMutation()
	if err := s.repository.RecordRetry(ctx, delivery, schedule); err != nil {
		finishIndexMutation(err)
		if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errScheduledMessageState
	}
	finishIndexMutation(s.upsertIndex(ctx, delivery.ID, nextAttempt))
	return nil
}

func (s *ScheduledMessageScheduler) finishGenerationFailure(ctx context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery, category string) error {
	return s.finishGenerationFailureAt(ctx, schedule, delivery, category, s.now().UTC())
}

func (s *ScheduledMessageScheduler) finishGenerationFailureAt(ctx context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery, category string, now time.Time) error {
	status := domainscheduledmessage.DeliveryStatusFailed
	next, nextErr := s.nextOccurrence(schedule, now)
	if schedule.Recurrence != domainscheduledmessage.RecurrenceOnce && nextErr == nil && next != nil {
		status = domainscheduledmessage.DeliveryStatusSkipped
		schedule.Status, schedule.NextScheduledAt = domainscheduledmessage.ScheduleStatusActive, &next.ScheduledAt
	} else {
		schedule.Status, schedule.NextScheduledAt = domainscheduledmessage.ScheduleStatusFailed, nil
		next = nil
	}
	delivery.Status, delivery.FailureCategory, delivery.NextAttemptAt = status, &category, nil
	delivery.UpdatedAt, schedule.UpdatedAt = now, now
	finishIndexMutation := s.beginIndexMutation()
	if err := s.repository.FinalizeOccurrence(ctx, delivery, schedule, next); err != nil {
		finishIndexMutation(err)
		if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errScheduledMessageState
	}
	finishIndexMutation(s.syncFinalIndex(ctx, delivery.ID, next))
	return nil
}

func (s *ScheduledMessageScheduler) finishDeliveryFailure(ctx context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery) error {
	return s.finishDeliveryFailureAt(ctx, schedule, delivery, s.now().UTC())
}

func (s *ScheduledMessageScheduler) finishDeliveryFailureAt(ctx context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery, now time.Time) error {
	next, nextErr := s.nextOccurrence(schedule, now)
	if schedule.Recurrence != domainscheduledmessage.RecurrenceOnce && nextErr == nil && next != nil {
		schedule.Status, schedule.NextScheduledAt = domainscheduledmessage.ScheduleStatusActive, &next.ScheduledAt
	} else {
		schedule.Status, schedule.NextScheduledAt = domainscheduledmessage.ScheduleStatusFailed, nil
		next = nil
	}
	category := deliveryFailure
	delivery.Status, delivery.FailureCategory, delivery.NextAttemptAt = domainscheduledmessage.DeliveryStatusFailed, &category, nil
	delivery.UpdatedAt, schedule.UpdatedAt = now, now
	finishIndexMutation := s.beginIndexMutation()
	if err := s.repository.FinalizeOccurrence(ctx, delivery, schedule, next); err != nil {
		finishIndexMutation(err)
		if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errScheduledMessageState
	}
	finishIndexMutation(s.syncFinalIndex(ctx, delivery.ID, next))
	return nil
}

func (s *ScheduledMessageScheduler) cancelInvalidTarget(ctx context.Context, schedule *domainscheduledmessage.Schedule, delivery *domainscheduledmessage.Delivery) error {
	if schedule == nil || delivery == nil || delivery.LeaseToken == nil {
		return nil
	}
	now := s.now().UTC()
	category := invalidTargetReason
	delivery.Status, delivery.FailureCategory, delivery.NextAttemptAt = domainscheduledmessage.DeliveryStatusCancelled, &category, nil
	delivery.UpdatedAt = now
	schedule.Status, schedule.NextScheduledAt, schedule.UpdatedAt = domainscheduledmessage.ScheduleStatusCancelled, nil, now
	finishIndexMutation := s.beginIndexMutation()
	if err := s.repository.FinalizeOccurrence(ctx, delivery, schedule, nil); err != nil {
		finishIndexMutation(err)
		if errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errScheduledMessageState
	}
	finishIndexMutation(s.removeFromIndex(ctx, delivery.ID))
	return nil
}

func (s *ScheduledMessageScheduler) nextOccurrence(schedule *domainscheduledmessage.Schedule, after time.Time) (*domainscheduledmessage.Delivery, error) {
	if schedule == nil || schedule.Recurrence == domainscheduledmessage.RecurrenceOnce {
		return nil, nil
	}
	nextAt, err := domainscheduledmessage.NextOccurrence(schedule.Recurrence, schedule.StartAt, after, schedule.Timezone)
	if err != nil || nextAt == nil {
		return nil, err
	}
	next := &domainscheduledmessage.Delivery{
		ID: utils.CreateUUID(), ScheduledMessageID: schedule.ID, ScheduledAt: *nextAt,
		NextAttemptAt: nextAt, Status: domainscheduledmessage.DeliveryStatusPending,
	}
	if schedule.ContentMode == domainscheduledmessage.ContentModeFixed {
		snapshot := schedule.Content
		next.ContentSnapshot = &snapshot
	}
	return next, nil
}

func scheduledMessageTarget(channel *domainscheduledmessage.SpaceChannel, identity *domainidentity.ExternalIdentity) (domainreminder.DeliveryTarget, error) {
	if channel != nil {
		provider := strings.ToLower(strings.TrimSpace(channel.Channel))
		if provider == "" || strings.TrimSpace(channel.ExternalChatID) == "" {
			return domainreminder.DeliveryTarget{}, interfacescheduledmessage.ErrInvalidTarget
		}
		return domainreminder.DeliveryTarget{Provider: provider, Target: channel.ExternalChatID}, nil
	}
	if identity == nil || strings.TrimSpace(identity.ExternalID) == "" || identity.Status != domainidentity.StatusActive {
		return domainreminder.DeliveryTarget{}, interfacescheduledmessage.ErrInvalidTarget
	}
	return domainreminder.DeliveryTarget{Provider: "whatsapp", Target: identity.ExternalID}, nil
}

func scheduledMessageText(schedule *domainscheduledmessage.Schedule, content string) string {
	if schedule.Recurrence == domainscheduledmessage.RecurrenceOnce {
		return content
	}
	lines := []string{content, ""}
	if schedule.Target == domainscheduledmessage.TargetDM {
		lines = append(lines, "Balas *unsubscribe* untuk berhenti menerima pesan ini.")
	} else {
		lines = append(lines, "Hanya admin grup yang dapat mengubah atau menghentikan pesan terjadwal ini.")
	}
	return strings.Join(lines, "\n")
}

func isInvalidScheduledMessageTarget(err error) bool {
	return errors.Is(err, interfacescheduledmessage.ErrInvalidTarget) || errors.Is(err, interfacescheduledmessage.ErrInvalidWhatsAppIdentity)
}

func (s *ScheduledMessageScheduler) syncFinalIndex(ctx context.Context, deliveryID string, next *domainscheduledmessage.Delivery) error {
	if next == nil {
		return s.removeFromIndex(ctx, deliveryID)
	}
	return errors.Join(s.removeFromIndex(ctx, deliveryID), s.upsertIndex(ctx, next.ID, next.ScheduledAt))
}

func (s *ScheduledMessageScheduler) beginIndexMutation() func(error) {
	if s.index == nil {
		return func(error) {
			// No index means there is no lifecycle mutation to finish.
		}
	}
	s.index.BeginLifecycleMutation()
	return s.index.FinishLifecycleMutation
}

func (s *ScheduledMessageScheduler) upsertIndex(ctx context.Context, deliveryID string, at time.Time) error {
	if s.index != nil {
		if err := s.index.Upsert(ctx, deliveryID, at); err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message due index upsert failed")
			return err
		}
	}
	return nil
}

func (s *ScheduledMessageScheduler) removeFromIndex(ctx context.Context, deliveryID string) error {
	if s.index != nil {
		if err := s.index.Remove(ctx, deliveryID); err != nil {
			logger.WriteLogWithContext(ctx, logger.LogLevelError, "scheduled message due index removal failed")
			return err
		}
	}
	return nil
}

func leaseCovers(delivery *domainscheduledmessage.Delivery, now time.Time, duration time.Duration) bool {
	return delivery != nil && delivery.LeaseExpiresAt != nil && delivery.LeaseExpiresAt.After(now.Add(duration))
}

func schedulerContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

var _ interfacescheduledmessage.Scheduler = (*ScheduledMessageScheduler)(nil)
