package condition_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type testArtifact struct{ source identity.Condition }

func (artifact testArtifact) Source() identity.Condition { return artifact.source }

type testPreparer func(identity.Condition, condition.PreparationLimits) (condition.CheckedCondition, error)

func (prepare testPreparer) PrepareCondition(source identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
	return prepare(source, limits)
}

type testEvaluator func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result

func (evaluate testEvaluator) EvaluateCondition(checked condition.CheckedCondition, attributes condition.AttributeSnapshot, limits condition.EvaluationLimits) condition.Result {
	return evaluate(checked, attributes, limits)
}

// sourcePreparer preserves source data in an opaque test artifact.
func sourcePreparer(source identity.Condition, _ condition.PreparationLimits) (condition.CheckedCondition, error) {
	return testArtifact{source: source}, nil
}

func TestConditionServicePassesExplicitInputsAndSemanticResults(t *testing.T) {
	t.Parallel()
	for _, expected := range []condition.Result{{Outcome: condition.Match}, {Outcome: condition.NoMatch}, {Reason: condition.ReasonMissingAttribute}, {Reason: condition.ReasonLimitExceeded}} {
		t.Run(string(expected.Reason)+string(rune('0'+expected.Outcome)), func(t *testing.T) {
			t.Parallel()
			settings := condition.DefaultConfig()
			settings.Timeout = time.Second
			attributes := condition.AttributeSnapshot{PrincipalID: "principal-1", RequestTime: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
			artifact := testArtifact{source: identity.Condition{Title: "Source"}}
			service, err := condition.New(testPreparer(sourcePreparer), testEvaluator(func(checked condition.CheckedCondition, supplied condition.AttributeSnapshot, limits condition.EvaluationLimits) condition.Result {
				if checked != artifact || supplied != attributes || limits != settings.Evaluation {
					t.Error("service changed explicit semantic inputs")
				}
				return expected
			}), settings)
			if err != nil {
				t.Fatal(err)
			}
			if actual := service.Evaluate(context.Background(), artifact, attributes); actual != expected {
				t.Fatalf("wrapped result=%+v, want %+v", actual, expected)
			}
		})
	}
}

func TestConditionServicePreparationUsesItsOwnPortAndLimits(t *testing.T) {
	t.Parallel()
	settings := condition.DefaultConfig()
	settings.Timeout = time.Second
	source := identity.Condition{Title: "Original", Expression: "true"}
	service, err := condition.New(testPreparer(func(supplied identity.Condition, limits condition.PreparationLimits) (condition.CheckedCondition, error) {
		if supplied != source || limits != settings.Preparation {
			t.Error("preparation source or limits changed")
		}
		return testArtifact{source: supplied}, nil
	}), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
		t.Fatal("preparation invoked predicate evaluation")
		return condition.Result{}
	}), settings)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := service.Prepare(context.Background(), source)
	if err != nil || checked == nil || checked.Source() != source {
		t.Fatalf("prepared source: checked=%v error=%v", checked, err)
	}
}

func TestConditionServiceRefusesCancellationBeforeAndAfterEvaluation(t *testing.T) {
	t.Parallel()
	for _, cancelBefore := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		var calls atomic.Int64
		service, err := condition.New(testPreparer(sourcePreparer), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
			calls.Add(1)
			cancel()
			return condition.Result{Outcome: condition.Match}
		}), condition.DefaultConfig())
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if cancelBefore {
			cancel()
		}
		actual := service.Evaluate(ctx, testArtifact{}, condition.AttributeSnapshot{})
		cancel()
		if actual.Outcome != condition.EvaluationError || actual.Reason != condition.ReasonCancelled {
			t.Fatalf("cancelled evaluation granted a result: %+v", actual)
		}
		if cancelBefore && calls.Load() != 0 {
			t.Fatal("already cancelled operation invoked the evaluator")
		}
	}
}

func TestConditionServiceDeadlineAndQueuedConcurrencyRefusal(t *testing.T) {
	t.Parallel()
	settings := condition.DefaultConfig()
	settings.Timeout, settings.MaxConcurrent = time.Second, 1
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	service, err := condition.New(testPreparer(sourcePreparer), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
		calls.Add(1)
		close(entered)
		<-release
		return condition.Result{Outcome: condition.Match}
	}), settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	finished := make(chan condition.Result, 1)
	go func() { finished <- service.Evaluate(ctx, testArtifact{}, condition.AttributeSnapshot{}) }()
	<-entered
	waiting, cancelWaiting := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelWaiting()
	if result := service.Evaluate(waiting, testArtifact{}, condition.AttributeSnapshot{}); result.Reason != condition.ReasonDeadlineExceeded {
		t.Errorf("queued refusal: %+v", result)
	}
	<-ctx.Done()
	close(release)
	if result := <-finished; result.Outcome != condition.EvaluationError || result.Reason != condition.ReasonDeadlineExceeded {
		t.Errorf("overdue semantic match escaped the wrapper: %+v", result)
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrency boundary admitted %d evaluations, want 1", calls.Load())
	}
}

func TestConditionServiceEnforcesItsOwnDeadlineAfterBoundedWork(t *testing.T) {
	t.Parallel()
	settings := condition.DefaultConfig()
	settings.Timeout = 10 * time.Millisecond
	service, err := condition.New(testPreparer(sourcePreparer), testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
		time.Sleep(2 * settings.Timeout)
		return condition.Result{Outcome: condition.Match}
	}), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result := service.Evaluate(context.Background(), testArtifact{}, condition.AttributeSnapshot{}); result.Reason != condition.ReasonDeadlineExceeded || result.Outcome != condition.EvaluationError {
		t.Fatalf("service deadline did not fence completed work: %+v", result)
	}
}

func TestConditionServiceConfigurationAndPreparationCancellation(t *testing.T) {
	t.Parallel()
	evaluator := testEvaluator(func(condition.CheckedCondition, condition.AttributeSnapshot, condition.EvaluationLimits) condition.Result {
		return condition.Result{Outcome: condition.Match}
	})
	for _, mutate := range []func(*condition.Config){
		func(settings *condition.Config) { settings.MaxConcurrent = 0 },
		func(settings *condition.Config) { settings.Timeout = 0 },
		func(settings *condition.Config) { settings.Preparation.MaxASTDepth = 0 },
		func(settings *condition.Config) { settings.Evaluation.CostBudget = 0 },
	} {
		settings := condition.DefaultConfig()
		mutate(&settings)
		if _, err := condition.New(testPreparer(sourcePreparer), evaluator, settings); err == nil {
			t.Error("invalid condition configuration accepted")
		}
	}
	if _, err := condition.New(nil, evaluator, condition.DefaultConfig()); err == nil {
		t.Error("missing preparation dependency accepted")
	}
	service, err := condition.New(testPreparer(sourcePreparer), evaluator, condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checked, err := service.Prepare(ctx, identity.Condition{Expression: "true"})
	var refusal condition.PreparationError
	if checked != nil || !errors.As(err, &refusal) || refusal.Reason != condition.ReasonCancelled {
		t.Fatalf("cancelled preparation: checked=%v error=%v", checked, err)
	}
}
