package config_test

import (
	"slices"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/platform/config"
)

type testFamily string

func (family *testFamily) UnmarshalText(value []byte) error {
	*family = testFamily("decoded-" + string(value))
	return nil
}

func TestEnvironmentLoaderBindsUndeclaredNestedKeysAndTypedValues(t *testing.T) {
	t.Setenv("PORTCULLIS_LOADER_TEST_DATABASE_FAMILY", "16")
	t.Setenv("PORTCULLIS_LOADER_TEST_TIMEOUT", "3s")
	t.Setenv("PORTCULLIS_LOADER_TEST_TAGS", "read,review")
	var settings struct {
		Database struct{ Family testFamily } `mapstructure:"database"`
		Timeout  time.Duration               `mapstructure:"timeout"`
		Tags     []string                    `mapstructure:"tags"`
	}
	if err := config.NewEnvironmentLoader("PORTCULLIS_LOADER_TEST").Unmarshal(&settings); err != nil {
		t.Fatal(err)
	}
	if settings.Database.Family != "decoded-16" || settings.Timeout != 3*time.Second || !slices.Equal(settings.Tags, []string{"read", "review"}) {
		t.Fatalf("typed environment binding failed: %+v", settings)
	}
}
