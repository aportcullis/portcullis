package condition_test

import (
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/condition"
)

func TestConditionConfigurationUsesTypedIsolatedEnvironment(t *testing.T) {
	t.Setenv("PORTCULLIS_CONDITION_PREPARATION_MAX_SOURCE_BYTES", "2048")
	t.Setenv("PORTCULLIS_CONDITION_PREPARATION_MAX_AST_NODES", "128")
	t.Setenv("PORTCULLIS_CONDITION_EVALUATION_COST_BUDGET", "500")
	t.Setenv("PORTCULLIS_CONDITION_TIMEOUT", "50ms")
	t.Setenv("PORTCULLIS_CONDITION_MAX_CONCURRENT", "4")
	settings, err := condition.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Preparation.MaxSourceBytes != 2048 || settings.Preparation.MaxASTNodes != 128 || settings.Evaluation.CostBudget != 500 || settings.Timeout != 50*time.Millisecond || settings.MaxConcurrent != 4 {
		t.Fatalf("condition environment did not bind typed values: %+v", settings)
	}
	t.Setenv("PORTCULLIS_CONDITION_TIMEOUT", "0s")
	if _, err := condition.LoadConfig(); err == nil {
		t.Fatal("unbounded timeout configuration accepted")
	}
}

func TestConditionConfigurationRejectsAbsoluteCeilingOverflow(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*condition.Config){
		func(settings *condition.Config) { settings.Preparation.MaxSourceBytes = condition.MaxSourceBytes + 1 },
		func(settings *condition.Config) { settings.Preparation.MaxASTNodes = condition.MaxASTNodes + 1 },
		func(settings *condition.Config) { settings.Preparation.MaxASTDepth = condition.MaxASTDepth + 1 },
		func(settings *condition.Config) { settings.Preparation.MaxListEntries = condition.MaxListEntries + 1 },
		func(settings *condition.Config) {
			settings.Evaluation.MaxAttributeBytes = condition.MaxAttributeBytes + 1
		},
		func(settings *condition.Config) { settings.Evaluation.CostBudget = condition.MaxCostBudget + 1 },
		func(settings *condition.Config) { settings.MaxConcurrent = condition.MaxConcurrent + 1 },
		func(settings *condition.Config) { settings.Timeout = time.Second + time.Nanosecond },
	} {
		settings := condition.DefaultConfig()
		mutate(&settings)
		if err := settings.Validate(); err == nil {
			t.Fatal("absolute configuration ceiling was ignored")
		}
	}
}
