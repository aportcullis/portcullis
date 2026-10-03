package dbtest_test

import (
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/dbtest"
)

func TestDatabaseConfigLoadsIndependentTargetsAndRequiredMode(t *testing.T) {
	t.Setenv("PORTCULLIS_TEST_DATABASE_URL", "postgres://metadata")
	t.Setenv("PORTCULLIS_TEST_TARGET_DATABASE_URL", "postgres://target")
	t.Setenv("PORTCULLIS_TEST_POSTGRES_FAMILY", "16")
	t.Setenv("PORTCULLIS_TEST_DATABASE_REQUIRED", "1")
	settings, err := dbtest.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if settings.MetadataURL != "postgres://metadata" || settings.TargetURL != "postgres://target" || settings.PostgresFamily != dbtest.PostgresFamily16 || !settings.Required {
		t.Fatal("typed database settings did not preserve independent roles and required mode")
	}
}

func TestDatabaseConfigRejectsInvalidRequiredFlagsAndUnreviewedFamilies(t *testing.T) {
	for _, scenario := range []struct{ name, key, value string }{
		{"invalid required flag", "PORTCULLIS_TEST_DATABASE_REQUIRED", "ture"},
		{"unreviewed target family", "PORTCULLIS_TEST_POSTGRES_FAMILY", "20"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv(scenario.key, scenario.value)
			if _, err := dbtest.LoadConfig(); err == nil {
				t.Fatal("invalid database test configuration accepted")
			}
		})
	}
}
