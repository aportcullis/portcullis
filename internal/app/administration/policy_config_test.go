package administration_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
)

func TestRolePolicyConfigurationBoundsFanoutAndLockWait(t *testing.T) {
	t.Setenv("PORTCULLIS_ROLE_POLICY_TIMEOUT", "3s")
	t.Setenv("PORTCULLIS_ROLE_POLICY_MAX_SUBJECTS", "7")
	settings, err := administration.LoadRolePolicyConfig()
	if err != nil || settings.Timeout != 3*time.Second || settings.MaxSubjects != 7 {
		t.Fatalf("typed isolated configuration: %+v %v", settings, err)
	}
	for _, scenario := range []struct{ key, value string }{
		{"PORTCULLIS_ROLE_POLICY_TIMEOUT", "0s"}, {"PORTCULLIS_ROLE_POLICY_TIMEOUT", "6s"},
		{"PORTCULLIS_ROLE_POLICY_MAX_SUBJECTS", "0"}, {"PORTCULLIS_ROLE_POLICY_MAX_SUBJECTS", "4097"}, {"PORTCULLIS_ROLE_POLICY_MAX_SUBJECTS", "invalid"},
	} {
		t.Run(scenario.key+scenario.value, func(t *testing.T) {
			t.Setenv(scenario.key, scenario.value)
			if _, err := administration.LoadRolePolicyConfig(); !errors.Is(err, administration.ErrInvalidBindingManagementConfig) {
				t.Fatalf("invalid settings accepted: %v", err)
			}
		})
	}
	if _, err := administration.NewAccountPolicyService(nil, nil, administration.DefaultBindingConfig()); err == nil {
		t.Fatal("missing account dependencies accepted")
	}
	if _, err := administration.NewRolePolicyService(nil, nil, administration.DefaultRolePolicyConfig()); err == nil {
		t.Fatal("missing role dependencies accepted")
	}
}
