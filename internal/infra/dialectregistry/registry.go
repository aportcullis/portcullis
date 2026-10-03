// Package dialectregistry selects registered SQL adapters for application ports.
package dialectregistry

import (
	"errors"
	"github.com/aportcullis/portcullis/internal/app/accessrequest"
	"github.com/aportcullis/portcullis/internal/app/execution"
	"github.com/aportcullis/portcullis/internal/domain/connection"
)

// Adapter provides the SQL behaviors used by submission and execution.
type Adapter interface {
	accessrequest.Dialect
	execution.Dialect
}

// Registration binds a stored engine to its qualified adapter.
type Registration struct {
	Engine  connection.DBType
	Adapter Adapter
}

// Registry holds immutable adapter registrations.
type Registry struct{ adapters map[connection.DBType]Adapter }

// New snapshots unique nonempty registrations without a fallback engine.
func New(registrations ...Registration) (*Registry, error) {
	r := &Registry{adapters: make(map[connection.DBType]Adapter, len(registrations))}
	for _, registration := range registrations {
		if registration.Engine == "" || registration.Adapter == nil {
			return nil, errors.New("dialectregistry: invalid registration")
		}
		if _, exists := r.adapters[registration.Engine]; exists {
			return nil, errors.New("dialectregistry: duplicate engine")
		}
		r.adapters[registration.Engine] = registration.Adapter
	}
	return r, nil
}

// SubmissionDialect selects the registered adapter for submission.
func (r *Registry) SubmissionDialect(engine connection.DBType) (accessrequest.Dialect, error) {
	adapter, exists := r.adapters[engine]
	if !exists {
		return nil, connection.ErrUnsupportedDBType
	}
	return adapter, nil
}

// ExecutionDialect selects the registered adapter for execution.
func (r *Registry) ExecutionDialect(engine connection.DBType) (execution.Dialect, error) {
	adapter, exists := r.adapters[engine]
	if !exists {
		return nil, connection.ErrUnsupportedDBType
	}
	return adapter, nil
}
