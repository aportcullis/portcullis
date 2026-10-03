package dbtest

import (
	"errors"

	"github.com/aportcullis/portcullis/internal/platform/config"
)

// PostgresFamily identifies a reviewed managed-target server family.
type PostgresFamily string

// Reviewed families match the explicit compatibility catalog.
const (
	PostgresFamily16 PostgresFamily = "16"
	PostgresFamily17 PostgresFamily = "17"
	PostgresFamily18 PostgresFamily = "18"
	PostgresFamily19 PostgresFamily = "19"
)

// UnmarshalText refuses families outside the reviewed compatibility window.
func (family *PostgresFamily) UnmarshalText(value []byte) error {
	requested := PostgresFamily(value)
	switch requested {
	case PostgresFamily16, PostgresFamily17, PostgresFamily18, PostgresFamily19:
		*family = requested
		return nil
	default:
		return errors.New("unreviewed PostgreSQL target family")
	}
}

// Config describes disposable metadata and managed-target fixtures.
type Config struct {
	MetadataURL    string         `mapstructure:"test_database_url"`
	TargetURL      string         `mapstructure:"test_target_database_url"`
	Required       bool           `mapstructure:"test_database_required"`
	PostgresFamily PostgresFamily `mapstructure:"test_postgres_family"`
}

// LoadConfig reads and validates the database harness environment.
func LoadConfig() (Config, error) {
	loader := config.NewEnvironmentLoader("PORTCULLIS")
	loader.SetDefault("test_postgres_family", string(PostgresFamily18))
	var settings Config
	if err := loader.Unmarshal(&settings); err != nil {
		return Config{}, errors.New("invalid database test configuration")
	}
	return settings, nil
}
