package loadconfig_test

import (
	"testing"

	"github.com/aportcullis/portcullis/tests/load/server/loadconfig"
)

func TestLoadHarnessSelectsItsConfiguredBinaryAndRunLabel(t *testing.T) {
	t.Setenv("LOAD_APP_BINARY", ".test-docker/benchmark/portcullis")
	t.Setenv("LOAD_RUN_LABEL", "review-run")
	t.Setenv("PORTCULLIS_APP_BINARY", "wrong-prefix")
	settings, err := loadconfig.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ApplicationBinary != ".test-docker/benchmark/portcullis" || settings.RunLabel != "review-run" {
		t.Fatal("harness ignored its binary or label settings")
	}
}

func TestLoadHarnessDefaultsEmptyOverrides(t *testing.T) {
	t.Setenv("LOAD_APP_BINARY", "")
	t.Setenv("LOAD_RUN_LABEL", "")
	settings, err := loadconfig.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ApplicationBinary != ".test-docker/e2e/portcullis" || settings.RunLabel != "" {
		t.Fatal("harness defaults changed")
	}
}
