package connection_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()
	p := connection.DefaultPolicy()

	if !p.Read.Allowed || p.Read.RequiredApprovals != 1 {
		t.Errorf("Read = %+v, want allowed with 1 approval", p.Read)
	}
	if p.Write.Allowed || p.Write.RequiredApprovals != 1 {
		t.Errorf("Write = %+v, want disallowed with 1 approval kept", p.Write)
	}
	if p.DDL.Allowed || p.DDL.RequiredApprovals != 1 {
		t.Errorf("DDL = %+v, want disallowed with 1 approval kept", p.DDL)
	}
	want := connection.Limits{QueryTimeoutSeconds: 30, MaxRows: 10_000, MaxResultBytes: 16 << 20}
	if p.Limits != want {
		t.Errorf("Limits = %+v, want %+v", p.Limits, want)
	}
	if p.Version != 1 {
		t.Errorf("Version = %d, want 1", p.Version)
	}
}

func validPolicyArgs() (connection.ConnectionID, connection.ClassRule, connection.ClassRule, connection.ClassRule, connection.Limits) {
	return "c1",
		connection.ClassRule{Allowed: true, RequiredApprovals: 1},
		connection.ClassRule{Allowed: false, RequiredApprovals: 2},
		connection.ClassRule{Allowed: false, RequiredApprovals: 3},
		connection.Limits{QueryTimeoutSeconds: 30, MaxRows: 10_000, MaxResultBytes: 16 << 20}
}

func TestNewPolicyValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	id, read, write, ddl, limits := validPolicyArgs()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		p, err := connection.NewPolicy(id, "org1", 2, read, write, ddl, limits, "u1", now)
		if err != nil {
			t.Fatalf("NewPolicy: %v", err)
		}
		if p.Version != 2 || p.CreatedAt != now || p.CreatedBy != "u1" {
			t.Errorf("policy = %+v", p)
		}
	})

	t.Run("boundaries hold", func(t *testing.T) {
		t.Parallel()
		edge := connection.Limits{QueryTimeoutSeconds: 300, MaxRows: 1, MaxResultBytes: 64 << 20}
		zeroApprovals := connection.ClassRule{Allowed: true, RequiredApprovals: 0}
		maxApprovals := connection.ClassRule{Allowed: true, RequiredApprovals: 100}
		if _, err := connection.NewPolicy(id, "org1", 1, zeroApprovals, maxApprovals, ddl, edge, "u1", now); err != nil {
			t.Errorf("boundary values must validate: %v", err)
		}
		low := connection.Limits{QueryTimeoutSeconds: 1, MaxRows: 10_000, MaxResultBytes: 4096}
		if _, err := connection.NewPolicy(id, "org1", 1, read, write, ddl, low, "u1", now); err != nil {
			t.Errorf("lower-boundary limits must validate: %v", err)
		}
	})

	invalid := []struct {
		name   string
		mutate func(*connection.ClassRule, *connection.Limits)
	}{
		{"negative approvals", func(r *connection.ClassRule, _ *connection.Limits) { r.RequiredApprovals = -1 }},
		{"approvals over cap", func(r *connection.ClassRule, _ *connection.Limits) { r.RequiredApprovals = 101 }},
		{"timeout zero", func(_ *connection.ClassRule, l *connection.Limits) { l.QueryTimeoutSeconds = 0 }},
		{"timeout over 5m", func(_ *connection.ClassRule, l *connection.Limits) { l.QueryTimeoutSeconds = 301 }},
		{"rows zero", func(_ *connection.ClassRule, l *connection.Limits) { l.MaxRows = 0 }},
		{"rows over cap", func(_ *connection.ClassRule, l *connection.Limits) { l.MaxRows = 10_001 }},
		{"bytes under floor", func(_ *connection.ClassRule, l *connection.Limits) { l.MaxResultBytes = 4095 }},
		{"bytes over quota", func(_ *connection.ClassRule, l *connection.Limits) { l.MaxResultBytes = (64 << 20) + 1 }},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cid, read, write, ddl, limits := validPolicyArgs()
			tt.mutate(&read, &limits)
			if _, err := connection.NewPolicy(cid, "org1", 1, read, write, ddl, limits, "u1", now); !errors.Is(err, connection.ErrInvalidPolicy) {
				t.Errorf("err = %v, want ErrInvalidPolicy", err)
			}
		})
	}

	t.Run("invalid ddl rule", func(t *testing.T) {
		t.Parallel()
		bad := connection.ClassRule{Allowed: true, RequiredApprovals: -1}
		if _, err := connection.NewPolicy(id, "org1", 1, read, write, bad, limits, "u1", now); !errors.Is(err, connection.ErrInvalidPolicy) {
			t.Errorf("err = %v, want ErrInvalidPolicy", err)
		}
	})

	missing := []struct {
		name string
		call func() error
	}{
		{"missing connection id", func() error {
			_, err := connection.NewPolicy("", "org1", 1, read, write, ddl, limits, "u1", now)
			return err
		}},
		{"missing organization", func() error {
			_, err := connection.NewPolicy(id, "", 1, read, write, ddl, limits, "u1", now)
			return err
		}},
		{"missing creator", func() error {
			_, err := connection.NewPolicy(id, "org1", 1, read, write, ddl, limits, "", now)
			return err
		}},
		{"version zero", func() error {
			_, err := connection.NewPolicy(id, "org1", 0, read, write, ddl, limits, "u1", now)
			return err
		}},
	}
	for _, tt := range missing {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.call(); !errors.Is(err, connection.ErrInvalidPolicy) {
				t.Errorf("err = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestPolicyRuleAndClasses(t *testing.T) {
	t.Parallel()
	id, read, write, ddl, limits := validPolicyArgs()
	p, err := connection.NewPolicy(id, "org1", 1, read, write, ddl, limits, "u1", time.Now())
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}

	if got := p.Rule(connection.ClassRead); got != read {
		t.Errorf("Rule(read) = %+v", got)
	}
	if got := p.Rule(connection.ClassWrite); got != write {
		t.Errorf("Rule(write) = %+v", got)
	}
	if got := p.Rule(connection.ClassDDL); got != ddl {
		t.Errorf("Rule(ddl) = %+v", got)
	}

	want := []connection.StatementClass{connection.ClassRead, connection.ClassWrite, connection.ClassDDL}
	got := connection.Classes()
	if len(got) != len(want) {
		t.Fatalf("Classes() = %v", got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Errorf("Classes()[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}
