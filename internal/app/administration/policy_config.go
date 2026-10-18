package administration

import (
	"time"

	platformconfig "github.com/aportcullis/portcullis/internal/platform/config"
)

// RolePolicyConfig bounds shared-role fanout memory and the complete locked transaction.
type RolePolicyConfig struct {
	Timeout     time.Duration `mapstructure:"timeout"`
	MaxSubjects int           `mapstructure:"max_subjects"`
}

// DefaultRolePolicyConfig returns finite shared-role mutation bounds.
func DefaultRolePolicyConfig() RolePolicyConfig {
	return RolePolicyConfig{Timeout: 2 * time.Second, MaxSubjects: 1024}
}

// Validate refuses absent or excessive deadlines and subject limits.
func (settings RolePolicyConfig) Validate() error {
	if err := (BindingConfig{Timeout: settings.Timeout}).Validate(); err != nil {
		return err
	}
	if settings.MaxSubjects <= 0 || settings.MaxSubjects > 4096 {
		return ErrInvalidBindingManagementConfig
	}
	return nil
}

// LoadRolePolicyConfig loads isolated shared-role policy transaction bounds.
func LoadRolePolicyConfig() (RolePolicyConfig, error) {
	loader := platformconfig.NewEnvironmentLoader("PORTCULLIS_ROLE_POLICY")
	defaults := DefaultRolePolicyConfig()
	loader.SetDefault("timeout", defaults.Timeout)
	loader.SetDefault("max_subjects", defaults.MaxSubjects)
	var settings RolePolicyConfig
	if err := loader.Unmarshal(&settings); err != nil {
		return RolePolicyConfig{}, ErrInvalidBindingManagementConfig
	}
	return settings, settings.Validate()
}
