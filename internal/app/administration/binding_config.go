package administration

import (
	"errors"
	"time"

	platformconfig "github.com/aportcullis/portcullis/internal/platform/config"
)

// Binding management errors distinguish preparation from storage availability.
var (
	ErrBindingManagementInactive      = errors.New("administration: binding management inactive")
	ErrBindingManagementUnavailable   = errors.New("administration: binding management unavailable")
	ErrInvalidBindingManagementConfig = errors.New("administration: invalid binding management configuration")
)

// BindingConfig bounds the entire management transaction and its lock waits.
type BindingConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
}

// DefaultBindingConfig returns the initial finite policy mutation deadline.
func DefaultBindingConfig() BindingConfig { return BindingConfig{Timeout: 2 * time.Second} }

// Validate rejects absent or excessive management deadlines.
func (settings BindingConfig) Validate() error {
	if settings.Timeout <= 0 || settings.Timeout > 5*time.Second {
		return ErrInvalidBindingManagementConfig
	}
	return nil
}

// LoadBindingConfig loads isolated policy management settings.
func LoadBindingConfig() (BindingConfig, error) {
	loader := platformconfig.NewEnvironmentLoader("PORTCULLIS_BINDING_MANAGEMENT")
	loader.SetDefault("timeout", DefaultBindingConfig().Timeout)
	var settings BindingConfig
	if err := loader.Unmarshal(&settings); err != nil {
		return BindingConfig{}, ErrInvalidBindingManagementConfig
	}
	return settings, settings.Validate()
}
