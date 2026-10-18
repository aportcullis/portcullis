package celcondition_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
)

// conditionSource creates versioned source data without hiding expression preparation.
func conditionSource(expression string) identity.Condition {
	return identity.Condition{Title: "Access window", Expression: expression, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
}

// attributeSnapshot supplies fixed trusted identities and an explicit decision time.
func attributeSnapshot() condition.AttributeSnapshot {
	return condition.AttributeSnapshot{PrincipalID: "user-1", PrincipalOrganizationID: "org-1", Resource: identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceRequest, ID: "request-1", ConnectionID: "conn-1"}, RequestTime: time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)}
}

// prepareExpression checks one expression with the default preparation budgets.
func prepareExpression(t testing.TB, adapter *celcondition.Adapter, expression string) condition.CheckedCondition {
	t.Helper()
	checked, err := adapter.PrepareCondition(conditionSource(expression), condition.DefaultConfig().Preparation)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return checked
}

func TestRestrictedCELPredicateScenarios(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, expression string
		outcome          condition.Outcome
	}{
		{"principal and organization", `principal.id == "user-1" && principal.organization_id == resource.organization_id`, condition.Match},
		{"bounded literal membership", `resource.connection_id in ["conn-1", "conn-2"]`, condition.Match},
		{"resource predicate", `resource.type == "request" && resource.id != "request-2"`, condition.Match},
		{"explicit historical time", `request.time >= timestamp("2020-01-01T00:00:00Z") && request.time < timestamp("2020-01-02T00:00:00Z")`, condition.Match},
		{"timestamp offset normalizes to supplied UTC", `request.time == timestamp("2020-01-01T21:00:00+09:00")`, condition.Match},
		{"false remains no match", `!(principal.id == "user-1") || false`, condition.NoMatch},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			checked := prepareExpression(t, adapter, scenario.expression)
			attributes := attributeSnapshot()
			before := attributes
			for range 5 {
				actual := adapter.EvaluateCondition(checked, attributes, condition.DefaultConfig().Evaluation)
				if actual.Outcome != scenario.outcome || actual.Reason != condition.ReasonNone {
					t.Fatalf("evaluation=%+v, want outcome %d", actual, scenario.outcome)
				}
			}
			if attributes != before || checked.Source() != conditionSource(scenario.expression) {
				t.Fatal("evaluation mutated its source or attribute inputs")
			}
		})
	}
}

func TestCELExpiryUsesFreshSnapshotAtExactBoundary(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	checked := prepareExpression(t, adapter, `request.time < timestamp("2020-01-01T12:00:00Z")`)
	for _, scenario := range []struct {
		offset  time.Duration
		outcome condition.Outcome
	}{{-time.Nanosecond, condition.Match}, {0, condition.NoMatch}, {time.Nanosecond, condition.NoMatch}} {
		attributes := attributeSnapshot()
		attributes.RequestTime = attributes.RequestTime.Add(scenario.offset)
		if result := adapter.EvaluateCondition(checked, attributes, condition.DefaultConfig().Evaluation); result.Outcome != scenario.outcome {
			t.Fatalf("expiry offset %s: %+v", scenario.offset, result)
		}
	}
}

func TestCELPreparationRejectsExpressionsOutsideContract(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, expression := range []string{
		``, `"non-boolean"`, `principal.unknown == "secret"`, `request.time < "2020-01-01"`,
		`["user-1"].exists(subject, subject == principal.id)`, `has(resource.connection_id)`,
		`resource.id.matches("secret")`, `resource.id.startsWith("request")`, `now() > request.time`,
		`http.get("secret") == true`, `dyn(principal.id) == "user-1"`, `1 + 1 == 2`,
		`principal.id < "user-2"`, `resource.id in [principal.id]`, `timestamp("secret") < request.time`,
		`timestamp("0000-01-01T00:00:00Z") < request.time`, `timestamp("0001-01-01T00:00:00+01:00") < request.time`,
		`resource["id"] == "secret"`, `true ? true : false`,
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			checked, err := adapter.PrepareCondition(conditionSource(expression), condition.DefaultConfig().Preparation)
			var refusal condition.PreparationError
			if checked != nil || !errors.As(err, &refusal) || refusal.Reason != condition.ReasonInvalidCondition {
				t.Fatalf("unsafe expression was not refused safely: checked=%v error=%v", checked != nil, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "resource.id") {
				t.Fatal("preparation exposed source or SDK diagnostics")
			}
		})
	}
}

func TestCELPreparationBudgetsAndVersions(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name       string
		expression string
		limits     condition.PreparationLimits
	}{
		{"source bytes", strings.Repeat(" ", 4097) + "true", condition.DefaultConfig().Preparation},
		{"node count", `true && false`, condition.PreparationLimits{MaxSourceBytes: 4096, MaxASTNodes: 2, MaxASTDepth: 32, MaxListEntries: 32}},
		{"tree depth", `!(!(!true))`, condition.PreparationLimits{MaxSourceBytes: 4096, MaxASTNodes: 256, MaxASTDepth: 2, MaxListEntries: 32}},
		{"list size", `principal.id in ["a", "b", "c"]`, condition.PreparationLimits{MaxSourceBytes: 4096, MaxASTNodes: 256, MaxASTDepth: 32, MaxListEntries: 2}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			_, err := adapter.PrepareCondition(conditionSource(scenario.expression), scenario.limits)
			var refusal condition.PreparationError
			if !errors.As(err, &refusal) || refusal.Reason != condition.ReasonLimitExceeded {
				t.Fatalf("preparation budget refusal: %v", err)
			}
		})
	}
	for _, field := range []string{"language", "environment"} {
		source := conditionSource("true")
		if field == "language" {
			source.LanguageVersion = "cel-v999"
		} else {
			source.EnvironmentVersion = "portcullis-v999"
		}
		_, err := adapter.PrepareCondition(source, condition.DefaultConfig().Preparation)
		var refusal condition.PreparationError
		if !errors.As(err, &refusal) || refusal.Reason != condition.ReasonUnsupportedVersion {
			t.Fatalf("unknown %s version: %v", field, err)
		}
	}
}

// foreignProgram cannot impersonate an adapter-owned checked artifact by returning plausible source.
type foreignProgram struct{}

func (foreignProgram) Source() identity.Condition { panic("foreign program callback must not run") }

func TestCELEvaluationRefusesMissingInputsAndBudgets(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, expression string
		change           func(*condition.AttributeSnapshot, *condition.EvaluationLimits)
		reason           condition.ReasonCode
	}{
		{"missing principal despite short circuit", `false && principal.id == "user-1"`, func(attributes *condition.AttributeSnapshot, _ *condition.EvaluationLimits) {
			attributes.PrincipalID = ""
		}, condition.ReasonMissingAttribute},
		{"true branch cannot hide missing principal", `true || principal.id == "user-1"`, func(attributes *condition.AttributeSnapshot, _ *condition.EvaluationLimits) {
			attributes.PrincipalID = ""
		}, condition.ReasonMissingAttribute},
		{"missing time", `request.time < timestamp("2030-01-01T00:00:00Z")`, func(attributes *condition.AttributeSnapshot, _ *condition.EvaluationLimits) {
			attributes.RequestTime = time.Time{}
		}, condition.ReasonMissingAttribute},
		{"inapplicable connection", `resource.connection_id == "conn-1"`, func(attributes *condition.AttributeSnapshot, _ *condition.EvaluationLimits) {
			attributes.Resource.ConnectionID = ""
		}, condition.ReasonMissingAttribute},
		{"deterministic cost", `principal.id == "user-1" && resource.id == "request-1"`, func(_ *condition.AttributeSnapshot, limits *condition.EvaluationLimits) { limits.CostBudget = 1 }, condition.ReasonLimitExceeded},
		{"attribute bytes", `principal.id == "user-1"`, func(attributes *condition.AttributeSnapshot, _ *condition.EvaluationLimits) {
			attributes.PrincipalID = identity.UserID(strings.Repeat("u", 257))
		}, condition.ReasonLimitExceeded},
		{"invalid limits", `true`, func(_ *condition.AttributeSnapshot, limits *condition.EvaluationLimits) { limits.CostBudget = 0 }, condition.ReasonInvalidLimits},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			checked := prepareExpression(t, adapter, scenario.expression)
			attributes, limits := attributeSnapshot(), condition.DefaultConfig().Evaluation
			scenario.change(&attributes, &limits)
			for range 3 {
				if result := adapter.EvaluateCondition(checked, attributes, limits); result.Outcome != condition.EvaluationError || result.Reason != scenario.reason {
					t.Fatalf("evaluation refusal=%+v, want %s", result, scenario.reason)
				}
			}
		})
	}
	for _, checked := range []condition.CheckedCondition{nil, foreignProgram{}} {
		if result := adapter.EvaluateCondition(checked, attributeSnapshot(), condition.DefaultConfig().Evaluation); result.Outcome != condition.EvaluationError || result.Reason != condition.ReasonUnsupportedProgram {
			t.Fatalf("forged checked artifact: %+v", result)
		}
	}
}

func TestCELCheckedArtifactSupportsConcurrentImmutableEvaluation(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	checked := prepareExpression(t, adapter, `principal.id == "user-1" && resource.connection_id in ["conn-1", "conn-2"]`)
	var callers sync.WaitGroup
	for callerIdx := range 16 {
		callers.Go(func() {
			limits := condition.DefaultConfig().Evaluation
			expected := condition.Result{Outcome: condition.Match}
			if callerIdx%2 == 0 {
				limits.CostBudget = 1
				expected = condition.Result{Reason: condition.ReasonLimitExceeded}
			}
			for range 20 {
				if result := adapter.EvaluateCondition(checked, attributeSnapshot(), limits); result != expected {
					t.Errorf("concurrent evaluation: %+v", result)
				}
			}
		})
	}
	callers.Wait()
}

func TestCELQuotedIdentityIsDataAndBudgetFailureDoesNotPoisonProgram(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	opaqueID := `user-1" || true || principal.id == "forged`
	checked := prepareExpression(t, adapter, "principal.id == "+strconv.Quote(opaqueID)+` && resource.id == "request-1"`)
	attributes, limits := attributeSnapshot(), condition.DefaultConfig().Evaluation
	attributes.PrincipalID = identity.UserID(opaqueID)
	lowBudget := limits
	lowBudget.CostBudget = 1
	if result := adapter.EvaluateCondition(checked, attributes, lowBudget); result.Reason != condition.ReasonLimitExceeded {
		t.Fatal(result)
	}
	if result := adapter.EvaluateCondition(checked, attributes, limits); result.Outcome != condition.Match {
		t.Fatalf("budget failure poisoned subsequent evaluation: %+v", result)
	}
	attributes.PrincipalID = "other-user"
	if result := adapter.EvaluateCondition(checked, attributes, limits); result.Outcome != condition.NoMatch {
		t.Fatalf("quoted identity escaped its string literal: %+v", result)
	}
}

func TestCELCheckedSourceCannotReplaceProgramAndRemainsPortableAcrossAdapters(t *testing.T) {
	t.Parallel()
	first, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	checked := prepareExpression(t, first, `principal.id == "other-user"`)
	copied := checked.Source()
	copied.Expression = "true"
	if checked.Source() == copied {
		t.Fatal("source copy changed the checked artifact")
	}
	for _, adapter := range []*celcondition.Adapter{first, second} {
		if result := adapter.EvaluateCondition(checked, attributeSnapshot(), condition.DefaultConfig().Evaluation); result.Outcome != condition.NoMatch {
			t.Fatalf("checked-program identity or deterministic environment changed: %+v", result)
		}
	}
}

func TestCELAdapterInjectedIntoOperationalConditionService(t *testing.T) {
	t.Parallel()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	settings := condition.DefaultConfig()
	settings.Timeout = time.Second
	service, err := condition.New(adapter, adapter, settings)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := service.Prepare(context.Background(), conditionSource(`resource.connection_id == "conn-1" && request.time < timestamp("2020-01-02T00:00:00Z")`))
	if err != nil {
		t.Fatal(err)
	}
	attributes := attributeSnapshot()
	if result := service.Evaluate(context.Background(), checked, attributes); result.Outcome != condition.Match {
		t.Fatal(result)
	}
	attributes.RequestTime = attributes.RequestTime.Add(24 * time.Hour)
	if result := service.Evaluate(context.Background(), checked, attributes); result.Outcome != condition.NoMatch {
		t.Fatal(result)
	}
}

func BenchmarkCELConditionEvaluation(b *testing.B) {
	adapter, err := celcondition.New()
	if err != nil {
		b.Fatal(err)
	}
	checked := prepareExpression(b, adapter, `principal.id == "user-1" && request.time < timestamp("2030-01-01T00:00:00Z") && resource.connection_id in ["conn-1", "conn-2"]`)
	attributes, limits := attributeSnapshot(), condition.DefaultConfig().Evaluation
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if result := adapter.EvaluateCondition(checked, attributes, limits); result.Outcome != condition.Match {
			b.Fatal(result)
		}
	}
}

func BenchmarkCELConditionPreparation(b *testing.B) {
	adapter, err := celcondition.New()
	if err != nil {
		b.Fatal(err)
	}
	source := conditionSource(`principal.id == "user-1" && request.time < timestamp("2030-01-01T00:00:00Z")`)
	limits := condition.DefaultConfig().Preparation
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := adapter.PrepareCondition(source, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCELBoundedListEvaluation(b *testing.B) {
	adapter, err := celcondition.New()
	if err != nil {
		b.Fatal(err)
	}
	literals := make([]string, condition.DefaultConfig().Preparation.MaxListEntries)
	for idx := range literals {
		literals[idx] = strconv.Quote("user-" + strconv.Itoa(idx))
	}
	predicate := "principal.id in [" + strings.Join(literals, ",") + "]"
	expression := strings.TrimSuffix(strings.Repeat(predicate+" && ", 6), " && ")
	checked := prepareExpression(b, adapter, expression)
	attributes, limits := attributeSnapshot(), condition.DefaultConfig().Evaluation
	attributes.PrincipalID = "user-31"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if result := adapter.EvaluateCondition(checked, attributes, limits); result.Outcome != condition.Match {
			b.Fatal(result)
		}
	}
}
