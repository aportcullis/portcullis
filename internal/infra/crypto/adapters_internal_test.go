package crypto

import "context"

// White-box (code.md): the cancellation/slot race in acquire cannot be steered
// through the public API — Hash occupies a slot only for the (uncontrollable)
// duration of a real hash — so these shims let the black-box tests fill and
// release the semaphore directly. Exports only, no test logic (the stdlib
// export_test idiom under this repo's *_internal_test.go naming).

func (h *Argon2Hasher) TestAcquire(ctx context.Context) error { return h.acquire(ctx) }

func (h *Argon2Hasher) TestFillSlot() { h.sem <- struct{}{} }

func (h *Argon2Hasher) TestReleaseSlot() { <-h.sem }

func (h *Argon2Hasher) TestSlotsInUse() int { return len(h.sem) }
