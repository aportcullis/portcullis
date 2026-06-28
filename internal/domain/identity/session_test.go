package identity_test

import (
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestSessionValidWindows(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := identity.NewSession("s1", "u1", now, 12*time.Hour, 7*24*time.Hour)

	if !s.Valid(now) {
		t.Error("fresh session should be valid")
	}
	if s.Valid(now.Add(13 * time.Hour)) {
		t.Error("session past idle expiry should be invalid")
	}
	if s.Valid(now.Add(8 * 24 * time.Hour)) {
		t.Error("session past absolute expiry should be invalid")
	}
}

func TestSessionRevoked(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := identity.NewSession("s1", "u1", now, 12*time.Hour, 7*24*time.Hour)
	revoked := now
	s.RevokedAt = &revoked

	if s.Valid(now) {
		t.Error("revoked session should be invalid")
	}
}

func TestUserActive(t *testing.T) {
	t.Parallel()
	active := identity.User{Status: identity.StatusActive}
	disabled := identity.User{Status: identity.StatusDisabled}
	if !active.Active() {
		t.Error("active user should report Active()")
	}
	if disabled.Active() {
		t.Error("disabled user should not report Active()")
	}
}
