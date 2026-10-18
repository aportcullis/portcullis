package condition

import (
	"time"

	platformconfig "github.com/aportcullis/portcullis/internal/platform/config"
)

// Absolute ceilings keep configuration finite and parser recursion safely bounded.
const (
	MaxSourceBytes    = 65536
	MaxASTNodes       = 4096
	MaxASTDepth       = 64
	MaxListEntries    = 256
	MaxCostBudget     = 100000
	MaxAttributeBytes = 4096
	MaxConcurrent     = 256
)

// Config contains preparation, deterministic evaluation and outer operational limits.
type Config struct {
	Preparation   PreparationLimits `mapstructure:"preparation"`
	Evaluation    EvaluationLimits  `mapstructure:"evaluation"`
	Timeout       time.Duration     `mapstructure:"timeout"`
	MaxConcurrent int               `mapstructure:"max_concurrent"`
}

// Validate refuses zero, unbounded and excessive parser budgets.
func (limits PreparationLimits) Validate() error {
	if limits.MaxSourceBytes <= 0 || limits.MaxSourceBytes > MaxSourceBytes || limits.MaxASTNodes <= 0 || limits.MaxASTNodes > MaxASTNodes ||
		limits.MaxASTDepth <= 0 || limits.MaxASTDepth > MaxASTDepth || limits.MaxListEntries <= 0 || limits.MaxListEntries > MaxListEntries {
		return PreparationError{Reason: ReasonInvalidLimits}
	}
	return nil
}

// Validate refuses zero, unbounded and excessive evaluation budgets.
func (limits EvaluationLimits) Validate() error {
	if limits.CostBudget == 0 || limits.CostBudget > MaxCostBudget || limits.MaxAttributeBytes <= 0 || limits.MaxAttributeBytes > MaxAttributeBytes {
		return PreparationError{Reason: ReasonInvalidLimits}
	}
	return nil
}

// Validate checks semantic and outer operational limits together.
func (settings Config) Validate() error {
	if settings.Preparation.Validate() != nil || settings.Evaluation.Validate() != nil || settings.Timeout <= 0 || settings.Timeout > time.Second || settings.MaxConcurrent <= 0 || settings.MaxConcurrent > MaxConcurrent {
		return PreparationError{Reason: ReasonInvalidLimits}
	}
	return nil
}

// LoadConfig reads isolated condition settings without loading operator files.
func LoadConfig() (Config, error) {
	defaults := DefaultConfig()
	loader := platformconfig.NewEnvironmentLoader("PORTCULLIS_CONDITION")
	loader.SetDefault("preparation.max_source_bytes", defaults.Preparation.MaxSourceBytes)
	loader.SetDefault("preparation.max_ast_nodes", defaults.Preparation.MaxASTNodes)
	loader.SetDefault("preparation.max_ast_depth", defaults.Preparation.MaxASTDepth)
	loader.SetDefault("preparation.max_list_entries", defaults.Preparation.MaxListEntries)
	loader.SetDefault("evaluation.cost_budget", defaults.Evaluation.CostBudget)
	loader.SetDefault("evaluation.max_attribute_bytes", defaults.Evaluation.MaxAttributeBytes)
	loader.SetDefault("timeout", defaults.Timeout)
	loader.SetDefault("max_concurrent", defaults.MaxConcurrent)
	var settings Config
	if err := loader.Unmarshal(&settings); err != nil {
		return Config{}, PreparationError{Reason: ReasonInvalidLimits}
	}
	return settings, settings.Validate()
}

// DefaultConfig returns the initial finite condition budgets.
func DefaultConfig() Config {
	return Config{
		Preparation: PreparationLimits{MaxSourceBytes: 4096, MaxASTNodes: 256, MaxASTDepth: 32, MaxListEntries: 32},
		Evaluation:  EvaluationLimits{CostBudget: 1000, MaxAttributeBytes: 256},
		Timeout:     20 * time.Millisecond, MaxConcurrent: 16,
	}
}
