// Package connection implements the connection-management use cases (M1, ADR-0014): registering target databases with a sealed credential, the mandatory server-side test-before-save, rename/config updates, and archive. It depends only on the domain and its consumer-defined ports.
package connection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/aportcullis/portcullis/internal/app/auditevent"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// New wires the service. Every dependency is required so a half-built service never starts.
func New(repo Repository, validator ConnectionValidator, codec CredentialCodec, auditor AuditRecorder) (*Service, error) {
	if repo == nil || validator == nil || codec == nil || auditor == nil {
		return nil, errors.New("connection: nil dependency (repo, validator, codec, and auditor are required)")
	}
	return &Service{
		repo: repo, validator: validator, codec: codec, auditor: auditor,
		logger: slog.Default(), now: time.Now, newID: uuid.NewString,
	}, nil
}

// WithClock overrides the time source (tests).
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// WithIDGenerator overrides the connection-id source (tests).
func (s *Service) WithIDGenerator(newID func() string) *Service { s.newID = newID; return s }

// WithLogger routes the service's own warnings (dropped audit events).
func (s *Service) WithLogger(l *slog.Logger) *Service { s.logger = l; return s }

// Create registers a connection: validate → test the target (refusing to persist on failure — PRD §7.2's test-before-save is enforced here, not in the UI) → seal the credential under the pre-generated id → insert with CONNECTION_CREATED (and CONNECTION_TLS_RELAXED for a relaxed mode) in one transaction.
func (s *Service) Create(ctx context.Context, actor identity.UserID, p CreateParams) (connection.Connection, error) {
	if err := connection.ValidateDisplayName(p.DisplayName); err != nil {
		return connection.Connection{}, err
	}
	env, err := connection.ParseEnvironment(p.Environment)
	if err != nil {
		return connection.Connection{}, err
	}
	if err := connection.ValidateDescription(p.Description); err != nil {
		return connection.Connection{}, err
	}
	target, mode, cred, err := parseConfig(p.Config)
	if err != nil {
		return connection.Connection{}, err
	}
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return connection.Connection{}, fmt.Errorf("resolve organization: %w", err)
	}

	id := connection.ConnectionID(s.newID())
	if err := s.test(ctx, actor, id, target, mode, cred, false); err != nil {
		return connection.Connection{}, err
	}
	// From here the target HAS been dialed: any failure below must still leave the access in the trail — the transactional CONNECTION_CREATED that would normally imply it rolls back with the failure (ADR-0014).

	conn, err := connection.New(id, org, connection.DBTypePostgreSQL, p.DisplayName, env, p.Description, target, mode, actor, s.now())
	if err != nil {
		s.recordUnsavedDial(ctx, actor, id, target, mode)
		return connection.Connection{}, err
	}
	sealed, err := s.codec.Seal(org, id, cred)
	if err != nil {
		s.recordUnsavedDial(ctx, actor, id, target, mode)
		return connection.Connection{}, fmt.Errorf("seal credential: %w", err)
	}

	events := []audit.Event{s.mutationEvent(ctx, actor, org, conn, audit.ActionConnectionCreated, map[string]any{
		"display_name": conn.DisplayName,
		"db_type":      string(conn.DBType),
		"environment":  string(conn.Environment),
		"tls_mode":     string(conn.TLSMode),
		"fingerprint":  conn.Fingerprint,
	})}
	if mode.Relaxed() {
		events = append(events, s.tlsRelaxedEvent(ctx, actor, org, conn))
	}
	if err := s.repo.Create(ctx, conn, sealed, events...); err != nil {
		s.recordUnsavedDial(ctx, actor, id, target, mode)
		return connection.Connection{}, err
	}
	return conn, nil
}

// Update edits a connection. A nil Config renames only (no test); a non-nil Config replaces the full target + credential after a fresh successful test (ADR-0014).
func (s *Service) Update(ctx context.Context, actor identity.UserID, id connection.ConnectionID, p UpdateParams) (connection.Connection, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return connection.Connection{}, fmt.Errorf("resolve organization: %w", err)
	}

	if p.Config == nil {
		existing, err := s.repo.GetByID(ctx, org, id)
		if err != nil {
			return connection.Connection{}, err
		}
		if err := guardVersion(existing, p.ExpectedVersion); err != nil {
			return connection.Connection{}, err
		}
		// The same keep-current contract as environment/description: an empty name keeps the stored one — all three descriptor fields behave alike in both update flows.
		name := p.DisplayName
		if name == "" {
			name = existing.DisplayName
		}
		if err := connection.ValidateDisplayName(name); err != nil {
			return connection.Connection{}, err
		}
		env, desc, err := resolveDescriptor(existing, p)
		if err != nil {
			return connection.Connection{}, err
		}
		fields := descriptorFields(existing, name, env, desc)
		evt := s.mutationEvent(ctx, actor, org, existing, audit.ActionConnectionUpdated, map[string]any{
			"fields":       fields,
			"display_name": name,
			"environment":  string(env),
		})
		return s.repo.UpdateDescriptor(ctx, org, id, name, env, desc, existing.Version, evt)
	}

	existing, err := s.repo.GetByID(ctx, org, id)
	if err != nil {
		return connection.Connection{}, err
	}
	if existing.IsArchived() {
		// Restore is a separate future flow (ADR-0014): an archived connection's credential is gone, and editing it back to life must stay explicit.
		return connection.Connection{}, connection.ErrArchived
	}
	if err := guardVersion(existing, p.ExpectedVersion); err != nil {
		return connection.Connection{}, err
	}
	name := p.DisplayName
	if name == "" {
		name = existing.DisplayName
	}
	if err := connection.ValidateDisplayName(name); err != nil {
		return connection.Connection{}, err
	}
	env, desc, err := resolveDescriptor(existing, p)
	if err != nil {
		return connection.Connection{}, err
	}
	target, mode, cred, err := parseConfig(*p.Config)
	if err != nil {
		return connection.Connection{}, err
	}
	// From here on the STORED id is authoritative, never the caller's: the adapter matches ids by uuid value, so a non-canonical spelling reaches this row while differing as a string — and the id is the AEAD AAD.
	if err := s.test(ctx, actor, existing.ID, target, mode, cred, false); err != nil {
		return connection.Connection{}, err
	}
	// The target has been dialed: failures below must still leave the access in the trail (see Create).
	sealed, err := s.codec.Seal(org, existing.ID, cred)
	if err != nil {
		s.recordUnsavedDial(ctx, actor, existing.ID, target, mode)
		return connection.Connection{}, fmt.Errorf("seal credential: %w", err)
	}

	expectedVersion := existing.Version
	updated := existing
	updated.DisplayName = name
	updated.Environment = env
	updated.Description = desc
	updated.Target = target
	updated.TLSMode = mode
	updated.Fingerprint = target.Fingerprint(existing.DBType)

	// "fields" names what actually changed: an empty or unchanged DisplayName keeps the current name (UpdateParams contract), so it is not a rename. display_name itself stays recorded as the final-name snapshot either way.
	fields := append([]string{"config"}, descriptorFields(existing, name, env, desc)...)
	events := []audit.Event{s.mutationEvent(ctx, actor, org, updated, audit.ActionConnectionUpdated, map[string]any{
		"fields":       fields,
		"display_name": updated.DisplayName,
		"environment":  string(updated.Environment),
		"tls_mode":     string(updated.TLSMode),
		"fingerprint":  updated.Fingerprint,
	})}
	if mode.Relaxed() {
		events = append(events, s.tlsRelaxedEvent(ctx, actor, org, updated))
	}
	replaced, err := s.repo.ReplaceConfig(ctx, updated, expectedVersion, sealed, events...)
	if err != nil {
		s.recordUnsavedDial(ctx, actor, existing.ID, target, mode)
		return connection.Connection{}, err
	}
	return replaced, nil
}

// resolveDescriptor preserves omitted descriptor fields. guardVersion compares the caller’s version with the stored row before either replacement flow to prevent lost updates.
func guardVersion(existing connection.Connection, expected int64) error {
	if existing.Version != expected {
		return connection.ErrConflict
	}
	return nil
}

func resolveDescriptor(existing connection.Connection, p UpdateParams) (connection.Environment, string, error) {
	env := existing.Environment
	if p.Environment != "" {
		var err error
		if env, err = connection.ParseEnvironment(p.Environment); err != nil {
			return "", "", err
		}
	}
	desc := existing.Description
	if p.Description != nil {
		desc = *p.Description
		if err := connection.ValidateDescription(desc); err != nil {
			return "", "", err
		}
	}
	return env, desc, nil
}

// descriptorFields records only changed fields while audit metadata retains the final descriptor snapshot.
func descriptorFields(existing connection.Connection, name string, env connection.Environment, desc string) []string {
	var fields []string
	if name != "" && name != existing.DisplayName {
		fields = append(fields, "display_name")
	}
	if env != existing.Environment {
		fields = append(fields, "environment")
	}
	if desc != existing.Description {
		fields = append(fields, "description")
	}
	return fields
}

// Archive soft-deletes the connection and discards its credential in one transaction with CONNECTION_ARCHIVED (PRD §4.3). The in-flight-execution guard is added by the executions slice (ADR-0014).
func (s *Service) Archive(ctx context.Context, actor identity.UserID, id connection.ConnectionID) (connection.Connection, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return connection.Connection{}, fmt.Errorf("resolve organization: %w", err)
	}
	// Archive's snapshot must describe the row actually archived, not a descriptor read before a concurrent config update. The Postgres adapter completes this event's metadata from ArchiveConnection RETURNING inside the same transaction.
	evt := s.newEvent(ctx, actor, audit.ActionConnectionArchived, audit.OutcomeSucceeded)
	evt.OrganizationID = org
	evt.TargetID = string(id)
	return s.repo.Archive(ctx, org, id, evt)
}

// Get returns one connection (descriptor only — the credential never leaves the store unsealed).
func (s *Service) Get(ctx context.Context, id connection.ConnectionID) (connection.Connection, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return connection.Connection{}, fmt.Errorf("resolve organization: %w", err)
	}
	return s.repo.GetByID(ctx, org, id)
}

// List returns the organization's connections.
func (s *Service) List(ctx context.Context, includeArchived bool) ([]connection.Connection, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve organization: %w", err)
	}
	return s.repo.List(ctx, org, includeArchived)
}

// TestByConfig runs the pre-save connection test against unsaved input. As an explicit test action it leaves a best-effort CONNECTION_TEST event either way (with no target id — nothing is saved yet).
func (s *Service) TestByConfig(ctx context.Context, actor identity.UserID, cfg ConfigInput) error {
	target, mode, cred, err := parseConfig(cfg)
	if err != nil {
		return err
	}
	return s.test(ctx, actor, "", target, mode, cred, true)
}

// TestByID re-tests a saved connection with its stored (sealed) credential.
func (s *Service) TestByID(ctx context.Context, actor identity.UserID, id connection.ConnectionID) error {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return fmt.Errorf("resolve organization: %w", err)
	}
	// One atomic read of descriptor + credential: never dial an old target with a freshly replaced credential (ADR-0014). TestMaterial already rejects an archived connection.
	conn, sealed, err := s.repo.TestMaterial(ctx, org, id)
	if err != nil {
		return err
	}
	// The STORED id is the AAD and the audit target — the caller's spelling may be a non-canonical uuid rendering of it (see Update).
	cred, err := s.codec.Open(org, conn.ID, sealed)
	if err != nil {
		return fmt.Errorf("open credential: %w", err)
	}
	return s.test(ctx, actor, conn.ID, conn.Target, conn.TLSMode, cred, true)
}

// test normalizes failures and records refused dials. Explicit tests record success here; successful saves include it in their transactional mutation event.
func (s *Service) test(ctx context.Context, actor identity.UserID, id connection.ConnectionID, target connection.Target, mode connection.TLSMode, cred connection.Credential, explicit bool) error {
	err := s.validator.ValidateConnection(ctx, target, mode, cred)
	if err == nil {
		if explicit {
			s.recordTest(ctx, actor, id, target, mode, audit.OutcomeSucceeded, "")
		}
		return nil
	}
	var te *connection.TestError
	if !errors.As(err, &te) {
		te = &connection.TestError{Bucket: connection.TestBucketFailed}
	}
	s.recordTest(ctx, actor, id, target, mode, audit.OutcomeFailed, string(te.Bucket))
	return te
}

// recordUnsavedDial leaves the best-effort trail for a dial whose save then failed: the transactional CONNECTION_CREATED/UPDATED that would normally imply the successful test rolled back with the mutation, but the target WAS accessed (ADR-0014; persisted=false marks the distinction).
func (s *Service) recordUnsavedDial(ctx context.Context, actor identity.UserID, id connection.ConnectionID, target connection.Target, mode connection.TLSMode) {
	e := s.newEvent(ctx, actor, audit.ActionConnectionTest, audit.OutcomeSucceeded)
	e.TargetID = string(id)
	e.Metadata = map[string]any{
		"fingerprint": target.Fingerprint(connection.DBTypePostgreSQL),
		"tls_mode":    string(mode),
		"persisted":   false,
	}
	s.recordBestEffort(ctx, e)
}

// recordTest writes the best-effort CONNECTION_TEST event for an explicit test action or a failed pre-save test. tls_mode is part of every CONNECTION_TEST (ADR-0014): these paths never emit CONNECTION_TLS_RELAXED, and a relaxed dial must stay audit-visible (PRD §8.1).
func (s *Service) recordTest(ctx context.Context, actor identity.UserID, id connection.ConnectionID, target connection.Target, mode connection.TLSMode, outcome audit.Outcome, reason string) {
	e := s.newEvent(ctx, actor, audit.ActionConnectionTest, outcome)
	e.TargetID = string(id)
	e.Metadata = map[string]any{
		"fingerprint": target.Fingerprint(connection.DBTypePostgreSQL),
		"tls_mode":    string(mode),
	}
	if reason != "" {
		e.Metadata["reason"] = reason
	}
	s.recordBestEffort(ctx, e)
}

// recordBestEffort persists an event detached from the request context so a client disconnect cannot erase it; a failure is logged, never surfaced.
func (s *Service) recordBestEffort(ctx context.Context, e audit.Event) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "audit event dropped", "action", e.Action, "reason", "resolve organization", "error_type", fmt.Sprintf("%T", err))
		return
	}
	e.OrganizationID = org
	if err := s.auditor.Record(ctx, e); err != nil {
		s.logger.WarnContext(ctx, "audit event dropped", "action", e.Action, "reason", "record", "error_type", fmt.Sprintf("%T", err))
	}
}

// mutationEvent assembles a transactional event for a state change: it rides the repository call and commits with it (ADR-0009). The organization is already resolved by the calling use case.
func (s *Service) mutationEvent(ctx context.Context, actor identity.UserID, org identity.OrganizationID, c connection.Connection, action audit.Action, metadata map[string]any) audit.Event {
	e := s.newEvent(ctx, actor, action, audit.OutcomeSucceeded)
	e.OrganizationID = org
	e.TargetID = string(c.ID)
	e.Metadata = metadata
	return e
}

func (s *Service) tlsRelaxedEvent(ctx context.Context, actor identity.UserID, org identity.OrganizationID, c connection.Connection) audit.Event {
	return s.mutationEvent(ctx, actor, org, c, audit.ActionConnectionTLSRelaxed, map[string]any{
		"tls_mode": string(c.TLSMode),
	})
}

func (s *Service) newEvent(ctx context.Context, actor identity.UserID, action audit.Action, outcome audit.Outcome) audit.Event {
	return auditevent.NewUser(ctx, actor, action, audit.TargetTypeConnection, outcome)
}

// parseConfig validates the wire input into domain value objects.
func parseConfig(cfg ConfigInput) (connection.Target, connection.TLSMode, connection.Credential, error) {
	mode, err := connection.ParseTLSMode(cfg.TLSMode)
	if err != nil {
		return connection.Target{}, "", connection.Credential{}, err
	}
	target, err := connection.NewTarget(cfg.Host, cfg.Port, cfg.Database)
	if err != nil {
		return connection.Target{}, "", connection.Credential{}, err
	}
	cred, err := connection.NewCredential(cfg.User, cfg.Password)
	if err != nil {
		return connection.Target{}, "", connection.Credential{}, err
	}
	return target, mode, cred, nil
}
