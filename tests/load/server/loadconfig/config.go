package loadconfig

import (
	"errors"

	"github.com/aportcullis/portcullis/internal/platform/config"
)

// Config identifies the application and its measurement label.
type Config struct {
	ApplicationBinary string `mapstructure:"app_binary"`
	RunLabel          string `mapstructure:"run_label"`
}

// Load reads the typed load-harness environment.
func Load() (Config, error) {
	loader := config.NewEnvironmentLoader("LOAD")
	loader.SetDefault("app_binary", ".test-docker/e2e/portcullis")
	var settings Config
	if err := loader.Unmarshal(&settings); err != nil {
		return Config{}, errors.New("invalid load harness configuration")
	}
	return settings, nil
}
