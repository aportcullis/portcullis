package logging

import (
	"slices"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/setting"
)

// The log_level vocabulary exists twice by construction: this package owns
// the runtime mapping (ADR-0010) and the domain settings registry declares it
// for descriptor validation (ADR-0017) — the domain imports nothing outward,
// so it cannot reference the map. This white-box test pins the two lists
// together; the map is unexported, so no black-box surface exists.
func TestLevelVocabularyMatchesSettingRegistry(t *testing.T) {
	t.Parallel()
	here := make([]string, 0, len(levels))
	for name := range levels {
		here = append(here, name)
	}
	slices.Sort(here)
	want := slices.Clone(setting.LogLevels)
	slices.Sort(want)
	if !slices.Equal(here, want) {
		t.Fatalf("logging levels %v != setting.LogLevels %v — update both together", here, want)
	}
}
