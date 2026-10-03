package config

import (
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// NewEnvironmentLoader creates an isolated environment reader for a typed configuration.
func NewEnvironmentLoader(prefix string) *viper.Viper {
	loader := viper.NewWithOptions(
		viper.ExperimentalBindStruct(),
		viper.EnvKeyReplacer(strings.NewReplacer(".", "_")),
		viper.WithDecodeHook(mapstructure.ComposeDecodeHookFunc(
			mapstructure.TextUnmarshallerHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		)),
	)
	loader.SetEnvPrefix(prefix)
	loader.AutomaticEnv()
	return loader
}
