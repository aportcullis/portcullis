package condition_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type preparationCompletion struct {
	checked condition.CheckedCondition
	err     error
}

// awaitPreparationCompletion bounds fixture coordination without asserting application latency.
func awaitPreparationCompletion(t *testing.T, finished <-chan preparationCompletion) preparationCompletion {
	t.Helper()
	select {
	case completion := <-finished:
		return completion
	case <-time.After(2 * time.Second):
		t.Fatal("preparation did not complete after the fixture released its producer")
		return preparationCompletion{}
	}
}

// requirePreparationRefusal checks that operational refusal exposes neither a usable artifact nor an ambiguous reason.
func requirePreparationRefusal(t *testing.T, completion preparationCompletion, reason condition.ReasonCode) {
	t.Helper()
	var refusal condition.PreparationError
	if completion.checked != nil || !errors.As(completion.err, &refusal) || refusal.Reason != reason {
		t.Fatalf("preparation refusal: checked=%v error=%v, want nil artifact and %s", completion.checked, completion.err, reason)
	}
}

func TestPreparationPreservesExplicitInputsArtifactAndConfiguredWorkWindow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := condition.DefaultConfig()
		settings.Timeout = time.Second
		settings.Preparation = condition.PreparationLimits{MaxSourceBytes: 512, MaxASTNodes: 32, MaxASTDepth: 8, MaxListEntries: 4}
		source := identity.Condition{Title: "Window", Expression: "true", LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
		expected := &testArtifact{source: source}
		service, err := condition.New(testPreparer(func(supplied identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
			if supplied != source || limits != settings.Preparation {
				t.Error("preparation changed its explicit source or configured limits")
			}
			synctest.Sleep(30 * time.Millisecond)
			return expected, nil
		}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
			t.Error("preparing source invoked predicate evaluation")
			return condition.Result{}
		}), settings)
		if err != nil {
			t.Fatal(err)
		}
		checked, err := service.Prepare(context.Background(), source)
		if err != nil || checked != expected || checked.Source() != source {
			t.Fatalf("configured preparation: checked=%v error=%v", checked, err)
		}
	})
}

func TestPreparationSemanticFailureReleasesAdmissionForFreshWork(t *testing.T) {
	t.Parallel()
	settings := condition.DefaultConfig()
	settings.Timeout, settings.MaxConcurrent = time.Second, 1
	expectedFailure := condition.PreparationError{Reason: condition.ReasonInvalidCondition}
	var calls int
	service, err := condition.New(testPreparer(func(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
		calls++
		if calls == 1 {
			return nil, expectedFailure
		}
		return sourcePreparer(source, limits)
	}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
		return condition.Result{Outcome: condition.Match}
	}), settings)
	if err != nil {
		t.Fatal(err)
	}
	source := identity.Condition{Title: "Fresh", Expression: "true"}
	checked, err := service.Prepare(context.Background(), source)
	if checked != nil || !errors.Is(err, expectedFailure) {
		t.Fatalf("semantic failure was changed: checked=%v error=%v", checked, err)
	}
	checked, err = service.Prepare(context.Background(), source)
	if err != nil || checked == nil || checked.Source() != source || calls != 2 {
		t.Fatalf("fresh preparation after failure: checked=%v calls=%d error=%v", checked, calls, err)
	}
	if result := service.Evaluate(context.Background(), checked, condition.AttributeSnapshot{}); result.Outcome != condition.Match {
		t.Fatalf("preparation retained its permit after completion: %+v", result)
	}
}

func TestPreparationAndEvaluationShareAdmissionUntilPreparationFinishes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := condition.DefaultConfig()
		settings.Timeout, settings.MaxConcurrent = time.Second, 1
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		releaseWork := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseWork()
		var evaluations atomic.Int64
		service, err := condition.New(testPreparer(func(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
			close(entered)
			<-release
			return sourcePreparer(source, limits)
		}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
			evaluations.Add(1)
			return condition.Result{Outcome: condition.Match}
		}), settings)
		if err != nil {
			t.Fatal(err)
		}
		source := identity.Condition{Expression: "true"}
		prepared := make(chan preparationCompletion, 1)
		go func() {
			checked, err := service.Prepare(context.Background(), source)
			prepared <- preparationCompletion{checked: checked, err: err}
		}()
		<-entered
		evaluated := make(chan condition.Result, 1)
		go func() {
			evaluated <- service.Evaluate(context.Background(), testArtifact{source: source}, condition.AttributeSnapshot{})
		}()
		synctest.Wait()
		if evaluations.Load() != 0 {
			t.Error("evaluation overlapped a preparer holding the only admission slot")
		}
		releaseWork()
		completion := awaitPreparationCompletion(t, prepared)
		if completion.err != nil || completion.checked == nil || completion.checked.Source() != source {
			t.Fatalf("admitted preparation: %+v", completion)
		}
		if result := <-evaluated; result.Outcome != condition.Match || evaluations.Load() != 1 {
			t.Fatalf("evaluation after preparation: %+v calls=%d", result, evaluations.Load())
		}
	})
}

func TestPreparationWaitsForAnAdmittedEvaluationBeforeInvokingItsProducer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := condition.DefaultConfig()
		settings.Timeout, settings.MaxConcurrent = time.Second, 1
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		releaseWork := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseWork()
		var preparations atomic.Int64
		service, err := condition.New(testPreparer(func(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
			preparations.Add(1)
			return sourcePreparer(source, limits)
		}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
			close(entered)
			<-release
			return condition.Result{Outcome: condition.Match}
		}), settings)
		if err != nil {
			t.Fatal(err)
		}
		evaluated := make(chan condition.Result, 1)
		go func() {
			evaluated <- service.Evaluate(context.Background(), testArtifact{}, condition.AttributeSnapshot{})
		}()
		<-entered
		source := identity.Condition{Expression: "true"}
		prepared := make(chan preparationCompletion, 1)
		go func() {
			checked, err := service.Prepare(context.Background(), source)
			prepared <- preparationCompletion{checked: checked, err: err}
		}()
		synctest.Wait()
		if preparations.Load() != 0 {
			t.Error("preparation overlapped an evaluator holding the only admission slot")
		}
		releaseWork()
		if result := <-evaluated; result.Outcome != condition.Match {
			t.Fatalf("admitted evaluation: %+v", result)
		}
		completion := awaitPreparationCompletion(t, prepared)
		if completion.err != nil || completion.checked == nil || completion.checked.Source() != source || preparations.Load() != 1 {
			t.Fatalf("preparation after evaluation: checked=%v error=%v calls=%d", completion.checked, completion.err, preparations.Load())
		}
	})
}

func TestPreparationRefusesAnAlreadyCancelledCallerWithoutInvokingItsProducer(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	service, err := condition.New(testPreparer(func(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
		calls.Add(1)
		return sourcePreparer(source, limits)
	}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
		return condition.Result{Outcome: condition.Match}
	}), condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checked, err := service.Prepare(ctx, identity.Condition{Expression: "true"})
	requirePreparationRefusal(t, preparationCompletion{checked: checked, err: err}, condition.ReasonCancelled)
	if calls.Load() != 0 {
		t.Fatalf("already cancelled preparation invoked its producer %d times", calls.Load())
	}
}

func TestPreparationRefusesCancellationWhileItsProducerFinishes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := condition.DefaultConfig()
		settings.Timeout, settings.MaxConcurrent = time.Second, 1
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		releaseWork := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseWork()
		service, err := condition.New(testPreparer(func(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
			if source.Title == "Held" {
				close(entered)
				<-release
			}
			return sourcePreparer(source, limits)
		}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
			return condition.Result{Outcome: condition.Match}
		}), settings)
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan preparationCompletion, 1)
		go func() {
			checked, err := service.Prepare(ctx, identity.Condition{Title: "Held", Expression: "true"})
			finished <- preparationCompletion{checked: checked, err: err}
		}()
		<-entered
		cancel()
		synctest.Wait()
		select {
		case completion := <-finished:
			t.Fatalf("preparation returned before its synchronous producer finished: %+v", completion)
		default:
		}
		releaseWork()
		requirePreparationRefusal(t, awaitPreparationCompletion(t, finished), condition.ReasonCancelled)
		checked, err := service.Prepare(context.Background(), identity.Condition{Title: "Fresh", Expression: "true"})
		if err != nil || checked == nil {
			t.Fatalf("cancelled producer retained its admission slot: checked=%v error=%v", checked, err)
		}
	})
}

func TestPreparationRefusesItsOwnDeadlineAfterItsProducerFinishes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := condition.DefaultConfig()
		settings.MaxConcurrent = 1
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		releaseWork := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseWork()
		service, err := condition.New(testPreparer(func(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
			if source.Title == "Held" {
				close(entered)
				<-release
			}
			return sourcePreparer(source, limits)
		}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
			return condition.Result{Outcome: condition.Match}
		}), settings)
		if err != nil {
			t.Fatal(err)
		}
		parent := context.Background()
		finished := make(chan preparationCompletion, 1)
		go func() {
			checked, err := service.Prepare(parent, identity.Condition{Title: "Held", Expression: "true"})
			finished <- preparationCompletion{checked: checked, err: err}
		}()
		<-entered
		synctest.Sleep(settings.Timeout + time.Nanosecond)
		if parent.Err() != nil {
			t.Fatal("the parent unexpectedly supplied the preparation deadline")
		}
		select {
		case completion := <-finished:
			t.Fatalf("deadline detached still-running preparation work: %+v", completion)
		default:
		}
		releaseWork()
		requirePreparationRefusal(t, awaitPreparationCompletion(t, finished), condition.ReasonDeadlineExceeded)
		checked, err := service.Prepare(parent, identity.Condition{Title: "Fresh", Expression: "true"})
		if err != nil || checked == nil {
			t.Fatalf("overdue producer retained its admission slot: checked=%v error=%v", checked, err)
		}
	})
}

func TestPreparationRefusesAQueuedDeadlineWithoutInvokingTheQueuedProducer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := condition.DefaultConfig()
		settings.Timeout, settings.MaxConcurrent = time.Second, 1
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		releaseWork := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseWork()
		var calls atomic.Int64
		service, err := condition.New(testPreparer(func(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
			calls.Add(1)
			if source.Title == "Holder" {
				close(entered)
				<-release
			}
			return sourcePreparer(source, limits)
		}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
			return condition.Result{Outcome: condition.Match}
		}), settings)
		if err != nil {
			t.Fatal(err)
		}
		holder := make(chan preparationCompletion, 1)
		go func() {
			checked, err := service.Prepare(context.Background(), identity.Condition{Title: "Holder", Expression: "true"})
			holder <- preparationCompletion{checked: checked, err: err}
		}()
		<-entered
		queuedCtx, cancelQueued := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancelQueued()
		queued := make(chan preparationCompletion, 1)
		go func() {
			checked, err := service.Prepare(queuedCtx, identity.Condition{Title: "Queued", Expression: "true"})
			queued <- preparationCompletion{checked: checked, err: err}
		}()
		synctest.Wait()
		synctest.Sleep(20 * time.Millisecond)
		requirePreparationRefusal(t, awaitPreparationCompletion(t, queued), condition.ReasonDeadlineExceeded)
		if calls.Load() != 1 {
			t.Fatalf("queued deadline invoked a producer: calls=%d, want only the holder", calls.Load())
		}
		releaseWork()
		completion := awaitPreparationCompletion(t, holder)
		if completion.err != nil || completion.checked == nil {
			t.Fatalf("queued refusal changed the admitted holder: %+v", completion)
		}
		checked, err := service.Prepare(context.Background(), identity.Condition{Title: "Fresh", Expression: "true"})
		if err != nil || checked == nil || calls.Load() != 2 {
			t.Fatalf("fresh preparation after queued refusal: checked=%v calls=%d error=%v", checked, calls.Load(), err)
		}
	})
}
