package executionguard

import (
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/sony/gobreaker/v2"
	"sync"
)

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
func (g *Guard) Allow(id connection.ConnectionID) (func(bool), error) {
	g.mu.Lock()
	breaker := g.breakers[id]
	if breaker == nil {
		breaker = gobreaker.NewTwoStepCircuitBreaker[any](gobreaker.Settings{Name: string(id)})
		g.breakers[id] = breaker
	}
	g.mu.Unlock()
	done, err := breaker.Allow()
	if err != nil {
		return nil, access.ErrTargetUnavailable
	}
	return func(success bool) {
		if success {
			done(nil)
		} else {
			done(access.ErrTargetUnavailable)
		}
	}, nil
}
