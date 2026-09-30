package logging

import (
	"slices"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/setting"
)

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
