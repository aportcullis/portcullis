package executionguard

import (
	"errors"
	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/sony/gobreaker/v2"
	"sync"
)

var errTargetNotAttempted = errors.New("executionguard: target not attempted")

// Guard maintains a process-local circuit breaker for each target connection.
type Guard struct {
	mu       sync.Mutex
	breakers map[connection.ConnectionID]*gobreaker.TwoStepCircuitBreaker[any]
}

// New builds the per-connection execution admission guard.
func New() *Guard {
	return &Guard{breakers: make(map[connection.ConnectionID]*gobreaker.TwoStepCircuitBreaker[any])}
}

// Allow admits one target attempt without acquiring any request lease.
func (g *Guard) Allow(id connection.ConnectionID) (func(execution.TargetOutcome), error) {
	g.mu.Lock()
	breaker := g.breakers[id]
	if breaker == nil {
		breaker = gobreaker.NewTwoStepCircuitBreaker[any](gobreaker.Settings{Name: string(id), IsExcluded: func(err error) bool { return errors.Is(err, errTargetNotAttempted) }})
		g.breakers[id] = breaker
	}
	g.mu.Unlock()
	done, err := breaker.Allow()
	if err != nil {
		return nil, access.ErrTargetUnavailable
	}
	return func(outcome execution.TargetOutcome) {
		switch outcome {
		case execution.TargetNotAttempted:
			done(errTargetNotAttempted)
		case execution.TargetHealthy:
			done(nil)
		default:
			done(access.ErrTargetUnavailable)
		}
	}, nil
}
