package pgdialect_test

import (
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/platform/config"
)

func TestBenchmarkSettingsSelectRequestedDatasetSize(t *testing.T) {
	t.Setenv("PORTCULLIS_BENCH_ROWS", "20000")
	settings, err := loadBenchmarkSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Rows != 20000 {
		t.Fatal("benchmark ignored requested dataset size")
	}
}

func TestBenchmarkSettingsDefaultEmptyDatasetSize(t *testing.T) {
	t.Setenv("PORTCULLIS_BENCH_ROWS", "")
	settings, err := loadBenchmarkSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Rows != 100000 {
		t.Fatal("benchmark default dataset changed")
	}
}

func TestBenchmarkSettingsRejectInvalidDatasetBeforeDatabaseStartup(t *testing.T) {
	for _, value := range []string{"typo", "9999", "1000001"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PORTCULLIS_BENCH_ROWS", value)
			if _, err := loadBenchmarkSettings(); err == nil {
				t.Fatal("invalid dataset size accepted")
			}
		})
	}
}

type benchmarkSettings struct {
	Rows int `mapstructure:"bench_rows"`
}

// loadBenchmarkSettings validates the dataset size before allocating a database.
func loadBenchmarkSettings() (benchmarkSettings, error) {
	loader := config.NewEnvironmentLoader("PORTCULLIS")
	loader.SetDefault("bench_rows", 100000)
	var settings benchmarkSettings
	if err := loader.Unmarshal(&settings); err != nil {
		return benchmarkSettings{}, errors.New("invalid benchmark configuration")
	}
	if settings.Rows < 10000 || settings.Rows > 1000000 {
		return benchmarkSettings{}, errors.New("PORTCULLIS_BENCH_ROWS must be between 10000 and 1000000")
	}
	return settings, nil
}
