package authz

import (
	"time"

	platformconfig "github.com/aportcullis/portcullis/internal/platform/config"
)

// MaxResourceBindings is the absolute candidate bound for a single resource decision.
const MaxResourceBindings = 1024

// ResourceConfig bounds database candidate reads and the entire permission checkpoint.
type ResourceConfig struct {
	MaxBindings int           `mapstructure:"max_bindings"`
	Timeout     time.Duration `mapstructure:"timeout"`
}

// DefaultResourceConfig returns finite initial resource decision budgets.
func DefaultResourceConfig() ResourceConfig {
	return ResourceConfig{MaxBindings: 64, Timeout: 250 * time.Millisecond}
}

// Validate refuses unbounded or excessive resource decision budgets.
func (settings ResourceConfig) Validate() error {
	if settings.MaxBindings <= 0 || settings.MaxBindings > MaxResourceBindings || settings.Timeout <= 0 || settings.Timeout > 5*time.Second {
		return ErrInvalidSnapshot
	}
	return nil
}

// LoadResourceConfig loads isolated resource authorization settings.
func LoadResourceConfig() (ResourceConfig, error) {
	defaults := DefaultResourceConfig()
	loader := platformconfig.NewEnvironmentLoader("PORTCULLIS_RESOURCE_AUTHORIZATION")
	loader.SetDefault("max_bindings", defaults.MaxBindings)
	loader.SetDefault("timeout", defaults.Timeout)
	var settings ResourceConfig
	if err := loader.Unmarshal(&settings); err != nil {
		return ResourceConfig{}, ErrInvalidSnapshot
	}
	return settings, settings.Validate()
}
