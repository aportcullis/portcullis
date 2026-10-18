package administration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// refusingConditionPreparer models operational and semantic source preparation failures.
type refusingConditionPreparer struct{ reason condition.ReasonCode }

func (preparer refusingConditionPreparer) Prepare(context.Context, identity.Condition) (condition.CheckedCondition, error) {
	return nil, condition.PreparationError{Reason: preparer.reason}
}

func TestBindingValidationDistinguishesUnavailablePreparationFromInvalidSource(t *testing.T) {
	for _, reason := range []condition.ReasonCode{condition.ReasonDeadlineExceeded, condition.ReasonCancelled, condition.ReasonInvalidCondition, condition.ReasonUnsupportedVersion, condition.ReasonLimitExceeded} {
		for _, operation := range []string{"validate", "create"} {
			t.Run(string(reason)+"/"+operation, func(t *testing.T) {
				repository, _, definition := bindingManagementScenario(t)
				service, err := administration.NewBindingService(repository, refusingConditionPreparer{reason: reason}, repository, administration.DefaultBindingConfig())
				if err != nil {
					t.Fatal(err)
				}
				if operation == "validate" {
					err = service.ValidateCondition(context.Background(), "org", "actor", bindingCondition(`true`))
				} else {
					definition.HasCondition = true
					definition.Condition = bindingCondition(`true`)
					_, err = service.Create(context.Background(), "org", "actor", definition)
				}
				expected := identity.ErrInvalidCondition
				if reason == condition.ReasonDeadlineExceeded || reason == condition.ReasonCancelled {
					expected = administration.ErrBindingManagementUnavailable
				}
				if !errors.Is(err, expected) || repository.state.revision != 1 {
					t.Fatalf("preparation classification: %v want %v revision=%d", err, expected, repository.state.revision)
				}
			})
		}
	}
}
