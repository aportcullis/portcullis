package crypto

import "context"

// White-box shims control hashing slots because the public API cannot schedule the cancellation/acquisition race.

func (h *Argon2Hasher) TestAcquire(ctx context.Context) error { return h.acquire(ctx) }

func (h *Argon2Hasher) TestFillSlot() { h.sem <- struct{}{} }

func (h *Argon2Hasher) TestReleaseSlot() { <-h.sem }

func (h *Argon2Hasher) TestSlotsInUse() int { return len(h.sem) }
