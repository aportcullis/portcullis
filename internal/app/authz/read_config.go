package authz

import (
	"time"

	platformconfig "github.com/aportcullis/portcullis/internal/platform/config"
)

// ReadConfig bounds coherent authorization work; history and capability targets use MaxCandidates per batch.
type ReadConfig struct {
	MaxCandidates int           `mapstructure:"max_candidates"`
	Timeout       time.Duration `mapstructure:"timeout"`
}

// DefaultReadConfig supplies finite initial collection and capability scan limits.
func DefaultReadConfig() ReadConfig { return ReadConfig{MaxCandidates: 1024, Timeout: 5 * time.Second} }

// Validate refuses unlimited or excessive coherent scans.
func (settings ReadConfig) Validate() error {
	if settings.MaxCandidates < 1 || settings.MaxCandidates > 65536 || settings.Timeout <= 0 || settings.Timeout > 30*time.Second {
		return ErrInvalidSnapshot
	}
	return nil
}

// LoadReadConfig reads isolated coherent scan settings.
func LoadReadConfig() (ReadConfig, error) {
	defaults := DefaultReadConfig()
	loader := platformconfig.NewEnvironmentLoader("PORTCULLIS_AUTHORIZATION_READ")
	loader.SetDefault("max_candidates", defaults.MaxCandidates)
	loader.SetDefault("timeout", defaults.Timeout)
	var settings ReadConfig
	if err := loader.Unmarshal(&settings); err != nil {
		return ReadConfig{}, ErrInvalidSnapshot
	}
	return settings, settings.Validate()
}
