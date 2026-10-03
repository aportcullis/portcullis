// Package accessrequest orchestrates draft submission, approvals, and visibility-scoped reads through injected ports (ADR-0018).
package accessrequest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aportcullis/portcullis/internal/app/auditevent"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/domain/setting"
)

// Service implements the access-request use cases.
type Service struct {
	repo     Repository
	targets  RequestTargets
	codec    PayloadCodec
	dialects DialectResolver
	validity time.Duration
	now      func() time.Time
	newID    func() string
}

// New builds PostgreSQL-only submission with the required ports.
func New(repo Repository, targets RequestTargets, codec PayloadCodec, dialect Dialect, validity time.Duration) (*Service, error) {
	if dialect == nil {
		return nil, errors.New("accessrequest: nil dialect")
	}
	return NewWithDialects(repo, targets, codec, postgresDialectResolver{dialect}, validity)
}

// NewWithDialects builds submission with engine-specific dialect selection.
func NewWithDialects(repo Repository, targets RequestTargets, codec PayloadCodec, dialects DialectResolver, validity time.Duration) (*Service, error) {
	if repo == nil || targets == nil || codec == nil || dialects == nil {
		return nil, errors.New("accessrequest: nil dependency (repo, targets, codec, and dialect are required)")
	}
	if validity < setting.MinApprovalValidity || validity > setting.MaxApprovalValidity {
		return nil, fmt.Errorf("accessrequest: approval validity %s outside [%s, %s]", validity, setting.MinApprovalValidity, setting.MaxApprovalValidity)
	}
	return &Service{repo: repo, targets: targets, codec: codec, dialects: dialects, validity: validity, now: time.Now, newID: uuid.NewString}, nil
}

// ListRequestableConnections returns active target summaries under requests.create without requiring connection-admin permissions.
func (s *Service) ListRequestableConnections(ctx context.Context) ([]access.RequestableConnection, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve organization: %w", err)
	}
	return s.targets.ListRequestable(ctx, org)
}

// WithClock overrides the time source (tests).
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// WithIDGenerator overrides the request-id source (tests).
func (s *Service) WithIDGenerator(newID func() string) *Service { s.newID = newID; return s }

// Create stores an encrypted draft and its creation audit event.
func (s *Service) Create(ctx context.Context, requester identity.UserID, p CreateParams) (access.RequestView, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("resolve organization: %w", err)
	}
	payload, err := access.NewDescribedPayload(p.Title, p.Body, p.SQL, p.Params)
	if err != nil {
		return access.RequestView{}, err
	}
	// No pre-read of the connection: CreateDraft locks the row and refuses an archived one inside its own transaction, the only check a concurrent archive cannot race (ADR-0018). A pre-read would add a round-trip and a false guarantee.
	r, err := access.NewDraft(access.RequestID(s.newID()), org, p.ConnectionID, requester, s.now())
	if err != nil {
		return access.RequestView{}, err
	}
	r.Title = payload.Title
	sealed, err := s.codec.Seal(org, r.ID, payload)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("seal payload: %w", err)
	}
	evt := s.newRequestAuditEvent(ctx, requester, audit.ActionAccessRequestCreated, r, "", access.StateDraft)
	view, err := s.repo.CreateDraft(ctx, r, sealed, evt)
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// UpdateDraft replaces the requester's draft payload after checking its version.
func (s *Service) UpdateDraft(ctx context.Context, requester identity.UserID, id access.RequestID, p UpdateDraftParams) (access.RequestView, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("resolve organization: %w", err)
	}
	r, _, err := s.repo.GetSealed(ctx, org, id)
	if err != nil {
		return access.RequestView{}, err
	}
	if r.RequesterID != requester {
		return access.RequestView{}, access.ErrNotFound
	}
	if r.State != access.StateDraft {
		return access.RequestView{}, access.ErrNotDraft
	}
	// Compare the client token with the loaded draft’s version before writing so unseen concurrent edits cannot be overwritten.
	if r.Version != p.ExpectedVersion {
		return access.RequestView{}, access.ErrConflict
	}
	payload, err := access.NewDescribedPayload(p.Title, p.Body, p.SQL, p.Params)
	if err != nil {
		return access.RequestView{}, err
	}
	r.Title = payload.Title
	sealed, err := s.codec.Seal(org, id, payload)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("seal payload: %w", err)
	}
	evt := s.newRequestAuditEvent(ctx, requester, audit.ActionAccessRequestUpdated, r, access.StateDraft, access.StateDraft)
	view, err := s.repo.UpdateDraft(ctx, r, sealed, r.Version, evt)
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// Submit seals the validated payload and transitions to pending or automatic approval.
func (s *Service) Submit(ctx context.Context, requester identity.UserID, id access.RequestID, expectedVersion int64) (access.RequestView, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("resolve organization: %w", err)
	}
	r, sealed, err := s.repo.GetSealed(ctx, org, id)
	if err != nil {
		return access.RequestView{}, err
	}
	if r.RequesterID != requester {
		return access.RequestView{}, access.ErrNotFound
	}
	if r.State != access.StateDraft {
		return access.RequestView{}, access.ErrNotDraft
	}
	// Guard the write with this loaded draft’s version so a concurrent edit cannot receive a snapshot computed from an older payload.
	if r.Version != expectedVersion {
		return access.RequestView{}, access.ErrConflict
	}
	payload, err := s.codec.Open(org, id, sealed)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("open payload: %w", err)
	}

	// The target read here IS the pin (ADR-0018): the policy version and quorum are copied onto the request (limits stay reachable through the pinned join), and so is the connection's CONFIG version — the id alone would let this approval outlive a replacement of the very database it names.
	target, err := s.repo.CurrentTarget(ctx, org, r.ConnectionID)
	if err != nil {
		return access.RequestView{}, err
	}
	dialect, err := s.dialects.SubmissionDialect(connection.DBType(target.DBType))
	if err != nil {
		return access.RequestView{}, err
	}
	if dialect == nil {
		return access.RequestView{}, connection.ErrUnsupportedDBType
	}

	// Bind named parameters with the stored target engine before parsing. Execution rebinds the sealed payload so approved and executed bytes match (ADR-0018).
	boundSQL, _, err := dialect.BindNamed(payload.SQL, payload.Params)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("%w: %w", access.ErrInvalidPayload, err)
	}

	// Parse → classify → map into the policy vocabulary. Anything the parser or classifier refuses is a submit failure — never a guess (§4.3).
	st, err := dialect.ParseSingle(boundSQL)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("%w: %w", access.ErrUnclassifiable, err)
	}
	parsedStatementClass, err := dialect.Classify(st)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("%w: %w", access.ErrUnclassifiable, err)
	}
	class, err := toConnectionStatementClass(parsedStatementClass)
	if err != nil {
		return access.RequestView{}, err
	}

	policy := target.Policy
	rule := policy.Rule(class)
	if !rule.Allowed {
		return access.RequestView{}, fmt.Errorf("%w: %s", access.ErrClassNotAllowed, class)
	}

	redaction, err := dialect.Redact(st)
	if err != nil {
		// Fail-closed is the redactor's contract (ADR-0016); a redaction failure refuses the submit rather than storing an unredactable SQL.
		return access.RequestView{}, fmt.Errorf("%w: %w", access.ErrInvalidPayload, err)
	}
	// The digest binds the FULL approval unit — org, requester, connection, pinned policy version, statement class, normalized SQL, and typed parameters — so any change to what was approved forces a new request (PRD §4.3, ADR-0018; OWASP transaction authorization). It is computed over the canonical serialization, before encryption/redaction (§8.4).
	canonical, err := access.CanonicalPayload(access.ApprovalUnit{
		OrganizationID:          org,
		RequesterID:             requester,
		ConnectionID:            r.ConnectionID,
		ConnectionConfigVersion: target.ConfigVersion,
		PolicyVersion:           policy.Version,
		Class:                   class,
		Title:                   r.Title,
		Body:                    payload.Body,
		SQL:                     payload.SQL,
		Params:                  payload.Params,
	})
	if err != nil {
		return access.RequestView{}, fmt.Errorf("canonical payload: %w", err)
	}
	digest, keyVersion, err := s.codec.Digest(canonical)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("digest payload: %w", err)
	}

	now := s.now()
	submitted, err := r.Submitted(access.Snapshot{
		Class:                   class,
		PolicyVersion:           policy.Version,
		ConnectionConfigVersion: target.ConfigVersion,
		ConnectionFingerprint:   target.Fingerprint,
		ConnectionDisplayName:   target.DisplayName,
		ConnectionDBType:        target.DBType,
		RequiredApprovals:       rule.RequiredApprovals,
		Digest:                  digest,
		DigestKeyVersion:        keyVersion,
		RedactedSQL:             redaction.SQL,
	}, now)
	if err != nil {
		return access.RequestView{}, err
	}

	events := []audit.Event{s.newSubmissionAuditEvent(ctx, requester, submitted, len(payload.Params))}
	autoApprove := rule.RequiredApprovals == 0
	final := submitted
	if autoApprove {
		if final, err = submitted.Approved(now, s.validity); err != nil {
			return access.RequestView{}, err
		}
		events = append(events, s.newAutomaticApprovalAuditEvent(ctx, final))
	}
	// r.Version, not expectedVersion: the guard belongs to the row this pipeline read (they are equal by the check above — passing r.Version is what keeps them equal if this code ever grows a re-read).
	view, err := s.repo.Submit(ctx, final, r.Version, s.validity, events...)
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// Approve records an eligible approval and applies the policy quorum.
func (s *Service) Approve(ctx context.Context, approver identity.UserID, id access.RequestID, reason string) (access.RequestView, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("resolve organization: %w", err)
	}
	evt := auditevent.NewUser(ctx, approver, audit.ActionAccessRequestApproved, audit.TargetTypeAccessRequest, audit.OutcomeSucceeded)
	view, err := s.repo.Approve(ctx, org, id, approver, reason, s.validity, evt)
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// Reject records a reasoned rejection and terminates the pending request.
func (s *Service) Reject(ctx context.Context, approver identity.UserID, id access.RequestID, reason string) (access.RequestView, error) {
	if strings.TrimSpace(reason) == "" {
		return access.RequestView{}, access.ErrReasonRequired
	}
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("resolve organization: %w", err)
	}
	evt := auditevent.NewUser(ctx, approver, audit.ActionAccessRequestRejected, audit.TargetTypeAccessRequest, audit.OutcomeSucceeded)
	view, err := s.repo.Reject(ctx, org, id, approver, reason, evt)
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// Cancel cancels the requester's draft, pending, or approved request.
func (s *Service) Cancel(ctx context.Context, requester identity.UserID, id access.RequestID) (access.RequestView, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("resolve organization: %w", err)
	}
	evt := auditevent.NewUser(ctx, requester, audit.ActionAccessRequestCancelled, audit.TargetTypeAccessRequest, audit.OutcomeSucceeded)
	view, err := s.repo.Cancel(ctx, org, id, requester, evt)
	if err != nil {
		return access.RequestView{}, err
	}
	return view, nil
}

// Get returns the request and decrypted payload within the caller's visibility scope.
func (s *Service) Get(ctx context.Context, viewer identity.UserID, canReviewAll bool, id access.RequestID) (access.RequestView, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("resolve organization: %w", err)
	}
	view, sealed, err := s.repo.Get(ctx, org, id)
	if err != nil {
		return access.RequestView{}, err
	}
	if !canReviewAll && view.Request.RequesterID != viewer {
		return access.RequestView{}, access.ErrNotFound
	}
	// Anyone who passes the visibility guard is authorized to decrypt (§8.4: requester or requests.approve holder), so the payload is always attached here — it was fetched with the view (no second query).
	payload, err := s.codec.Open(org, id, sealed)
	if err != nil {
		return access.RequestView{}, fmt.Errorf("open payload: %w", err)
	}
	view.Payload = &payload
	return view, nil
}

// List returns a page of requests visible to the caller.
func (s *Service) List(ctx context.Context, viewer identity.UserID, canReviewAll bool, q access.ListQuery) (access.RequestPage, error) {
	org, err := s.repo.DefaultOrganizationID(ctx)
	if err != nil {
		return access.RequestPage{}, fmt.Errorf("resolve organization: %w", err)
	}
	if q.Page < 1 {
		q.Page = 1
	}
	// page_size is an allowlist ({10,20,50,100}), not a clamp — an off-list value selects the default (proto/PRD §7.1; the audit list's stance).
	if !allowedPageSizes[q.PageSize] {
		q.PageSize = defaultPageSize
	}
	if q.State != "" {
		valid := false
		for _, st := range access.States() {
			if st == q.State {
				valid = true
			}
		}
		if !valid {
			return access.RequestPage{}, access.ErrInvalidRequest
		}
	}
	if !canReviewAll {
		q.RequesterID = viewer
	}
	page, err := s.repo.List(ctx, org, q)
	if err != nil {
		return access.RequestPage{}, err
	}
	// The repository clamps pages and computes effective states within one database snapshot; recomputing here could disagree with the returned rows.
	page.PageSize = q.PageSize
	page.TotalPages = calculateTotalPages(page.TotalCount, q.PageSize)
	return page, nil
}

// newRequestAuditEvent builds a request transition audit event.
func (s *Service) newRequestAuditEvent(ctx context.Context, actor identity.UserID, action audit.Action, r access.Request, from, to access.State) audit.Event {
	evt := auditevent.NewUser(ctx, actor, action, audit.TargetTypeAccessRequest, audit.OutcomeSucceeded)
	evt.OrganizationID = r.OrganizationID
	evt.TargetID = string(r.ID)
	evt.PreviousState = string(from)
	evt.NextState = string(to)
	evt.ConnectionID = string(r.ConnectionID)
	evt.QueryType = string(r.Class)
	evt.PayloadDigest = r.Digest
	evt.PayloadDigestKeyVersion = r.DigestKeyVersion
	evt.Metadata = map[string]any{"connection_id": string(r.ConnectionID)}
	return evt
}

// newSubmissionAuditEvent records the submitted payload and target snapshot.
func (s *Service) newSubmissionAuditEvent(ctx context.Context, requester identity.UserID, r access.Request, parameterCount int) audit.Event {
	evt := s.newRequestAuditEvent(ctx, requester, audit.ActionAccessRequestSubmitted, r, access.StateDraft, access.StatePending)
	evt.Metadata = map[string]any{
		"connection_id":      string(r.ConnectionID),
		"redacted_sql":       r.RedactedSQL,
		"statement_class":    string(r.Class),
		"policy_version":     r.PolicyVersion,
		"required_approvals": r.RequiredApprovals,
		"parameter_count":    parameterCount,
		// Pin the submitted target’s config version and descriptor so later edits cannot rewrite approval history (PRD §4.3).
		"connection_display_name":   r.ConnectionDisplayName,
		"connection_db_type":        r.ConnectionDBType,
		"connection_fingerprint":    r.ConnectionFingerprint,
		"connection_config_version": r.ConnectionConfigVersion,
	}
	return evt
}

// newAutomaticApprovalAuditEvent records a zero-quorum system approval.
func (s *Service) newAutomaticApprovalAuditEvent(ctx context.Context, r access.Request) audit.Event {
	evt := s.newRequestAuditEvent(ctx, "", audit.ActionAccessRequestApproved, r, access.StatePending, access.StateApproved)
	evt.ActorType = audit.ActorSystem
	evt.ActorUserID = nil
	evt.ActorService = access.ActorAutoApproval
	evt.Metadata = map[string]any{
		"connection_id":      string(r.ConnectionID),
		"required_approvals": 0,
		// The store sets approval expiry after acquiring the connection lock. Each submitted event carries its own redacted SQL evidence.
		"redacted_sql": r.RedactedSQL,
	}
	return evt
}

// toConnectionStatementClass converts a parsed statement class into its policy class.
func toConnectionStatementClass(c query.StatementClass) (connection.StatementClass, error) {
	switch c {
	case query.ClassRead:
		return connection.ClassRead, nil
	case query.ClassWrite:
		return connection.ClassWrite, nil
	case query.ClassDDL:
		return connection.ClassDDL, nil
	}
	return "", access.ErrUnclassifiable
}

func calculateTotalPages(total int64, pageSize int) int {
	if total <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(pageSize)))
}
