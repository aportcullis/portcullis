package logging

import (
	"context"
	"fmt"
	"log/slog"
)

// PrintfLogger adapts a printf-style logger — the shape libraries such as testcontainers expect — onto slog, so third-party output shares the one standardized structured format instead of its own ad-hoc lines.
type PrintfLogger struct {
	logger    *slog.Logger
	level     slog.Level
	component string
}

// NewPrintfLogger routes Printf calls to logger at the given level, tagged with a component name so the source is identifiable in the unified stream.
func NewPrintfLogger(logger *slog.Logger, level slog.Level, component string) *PrintfLogger {
	return &PrintfLogger{logger: logger, level: level, component: component}
}

// Printf satisfies the printf-style logger interface used by third-party libs.
func (p *PrintfLogger) Printf(format string, v ...any) {
	p.logger.LogAttrs(context.Background(), p.level,
		fmt.Sprintf(format, v...),
		slog.String("component", p.component),
	)
}
