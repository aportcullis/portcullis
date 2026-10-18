package authz

import (
	"context"
	"sync"
)

type outputFenceKey struct{}
type outputFence struct {
	mutex  sync.Mutex
	checks []func(context.Context) error
}

// WithOutputFence owns fresh checks for one response, never a reusable capability.
func WithOutputFence(ctx context.Context) context.Context {
	return context.WithValue(ctx, outputFenceKey{}, &outputFence{})
}

// WithoutOutputFence prevents a fresh output validation from registering itself recursively.
func WithoutOutputFence(ctx context.Context) context.Context {
	return context.WithValue(ctx, outputFenceKey{}, (*outputFence)(nil))
}

// RegisterOutputCheck records a bounded current-authority recheck owned by a trusted reader.
func RegisterOutputCheck(ctx context.Context, check func(context.Context) error) {
	fence, ok := ctx.Value(outputFenceKey{}).(*outputFence)
	if !ok || fence == nil {
		return
	}
	fence.mutex.Lock()
	defer fence.mutex.Unlock()
	fence.checks = append(fence.checks, check)
}

// CheckOutput fails the entire response if any fresh authority/data consistency check fails.
func CheckOutput(ctx context.Context) error {
	fence, ok := ctx.Value(outputFenceKey{}).(*outputFence)
	if !ok || fence == nil {
		return nil
	}
	fence.mutex.Lock()
	checks := append([]func(context.Context) error(nil), fence.checks...)
	fence.mutex.Unlock()
	for _, check := range checks {
		if err := check(WithoutOutputFence(ctx)); err != nil {
			return err
		}
	}
	return ctx.Err()
}
