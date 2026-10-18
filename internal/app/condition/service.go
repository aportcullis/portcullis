package condition

import (
	"context"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Service owns cancellation, deadlines and concurrency outside the pure evaluator.
type Service struct {
	preparer  Preparer
	evaluator Evaluator
	settings  Config
	permits   chan struct{}
}

// New injects separately owned preparation and evaluation ports with validated limits.
func New(preparer Preparer, evaluator Evaluator, settings Config) (*Service, error) {
	if preparer == nil || evaluator == nil {
		return nil, errors.New("condition: missing dependency")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &Service{preparer: preparer, evaluator: evaluator, settings: settings, permits: make(chan struct{}, settings.MaxConcurrent)}, nil
}

// Prepare checks source before future authorization decisions consume the returned artifact.
func (service *Service) Prepare(ctx context.Context, source identity.Condition) (CheckedCondition, error) {
	bounded, cancel := context.WithTimeout(ctx, service.settings.Timeout)
	defer cancel()
	if reason := service.acquirePermit(bounded); reason != ReasonNone {
		return nil, PreparationError{Reason: reason}
	}
	defer service.releasePermit()
	checked, err := service.preparer.PrepareCondition(source, service.settings.Preparation)
	if reason := contextFailure(bounded); reason != ReasonNone {
		return nil, PreparationError{Reason: reason}
	}
	return checked, err
}

// Evaluate refuses cancelled or overdue calls and bounds active pure invocations.
func (service *Service) Evaluate(ctx context.Context, checked CheckedCondition, attributes AttributeSnapshot) Result {
	bounded, cancel := context.WithTimeout(ctx, service.settings.Timeout)
	defer cancel()
	if reason := service.acquirePermit(bounded); reason != ReasonNone {
		return Result{Reason: reason}
	}
	defer service.releasePermit()
	result := service.evaluator.EvaluateCondition(checked, attributes, service.settings.Evaluation)
	if reason := contextFailure(bounded); reason != ReasonNone {
		return Result{Reason: reason}
	}
	return result
}

// acquirePermit bounds concurrent preparation/evaluation and fences cancellation races.
func (service *Service) acquirePermit(ctx context.Context) ReasonCode {
	if reason := contextFailure(ctx); reason != ReasonNone {
		return reason
	}
	select {
	case service.permits <- struct{}{}:
		if reason := contextFailure(ctx); reason != ReasonNone {
			service.releasePermit()
			return reason
		}
		return ReasonNone
	case <-ctx.Done():
		return contextFailure(ctx)
	}
}

// releasePermit releases a completed bounded call without spawning background work.
func (service *Service) releasePermit() { <-service.permits }

// contextFailure classifies operational refusal outside semantic evaluation.
func contextFailure(ctx context.Context) ReasonCode {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ReasonDeadlineExceeded
	case ctx.Err() != nil:
		return ReasonCancelled
	default:
		if deadline, exists := ctx.Deadline(); exists && !time.Now().Before(deadline) {
			return ReasonDeadlineExceeded
		}
		return ReasonNone
	}
}
