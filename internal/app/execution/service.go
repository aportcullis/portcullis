package execution

import (
	"context"
	"errors"
	"github.com/aportcullis/portcullis/internal/app/auditevent"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/google/uuid"
	"sync"
	"time"
)

// Service orchestrates single-use approved execution through injected adapters.
type Service struct {
	requests      RequestRepository
	leases        LeaseRepository
	connections   ConnectionRepository
	payloads      PayloadCodec
	credentials   CredentialCodec
	dialects      DialectResolver
	results       ResultWriter
	admission     Admission
	owner         string
	workers       chan struct{}
	mu            sync.Mutex
	draining      bool
	cancellations map[access.RequestID]context.CancelFunc
}

// New wires governed execution with a bounded number of active workers.
func New(requests RequestRepository, leases LeaseRepository, connections ConnectionRepository, payloads PayloadCodec, credentials CredentialCodec, dialect Dialect, results ResultWriter, owner string, workers int) (*Service, error) {
	if dialect == nil {
		return nil, errors.New("execution: nil dialect")
	}
	return NewWithDialects(requests, leases, connections, payloads, credentials, postgresDialectResolver{dialect}, results, owner, workers)
}

// NewWithDialects wires execution with registered engine-specific adapters.
func NewWithDialects(requests RequestRepository, leases LeaseRepository, connections ConnectionRepository, payloads PayloadCodec, credentials CredentialCodec, dialects DialectResolver, results ResultWriter, owner string, workers int) (*Service, error) {
	if requests == nil || leases == nil || connections == nil || payloads == nil || credentials == nil || dialects == nil || results == nil || owner == "" || workers < 1 {
		return nil, errors.New("execution: invalid dependencies")
	}
	return &Service{requests: requests, leases: leases, connections: connections, payloads: payloads, credentials: credentials, dialects: dialects, results: results, owner: owner, workers: make(chan struct{}, workers), cancellations: make(map[access.RequestID]context.CancelFunc)}, nil
}

// Execute runs only the authenticated stored approval unit, once.
func (s *Service) Execute(ctx context.Context, requester identity.UserID, id access.RequestID) (completion access.ExecutionCompletion, err error) {
	org, err := s.requests.DefaultOrganizationID(ctx)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	r, sealed, err := s.requests.GetSealed(ctx, org, id)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	if r.RequesterID != requester {
		return access.ExecutionCompletion{}, access.ErrNotFound
	}
	leased := false
	defer func() {
		if err == nil || leased {
			return
		}
		refusalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		event := auditevent.NewUser(ctx, requester, audit.Action("EXECUTION_REJECTED"), audit.TargetTypeAccessRequest, audit.OutcomeFailed)
		if auditErr := s.leases.RecordExecutionRefusal(refusalCtx, org, id, requester, "preflight_refused", event); auditErr != nil {
			err = auditErr
		}
	}()
	s.mu.Lock()
	draining := s.draining
	s.mu.Unlock()
	if draining {
		return access.ExecutionCompletion{}, query.ErrResultBusy
	}
	select {
	case s.workers <- struct{}{}:
		defer func() { <-s.workers }()
	default:
		return access.ExecutionCompletion{}, query.ErrResultBusy
	}
	if r.State != access.StateApproved {
		return access.ExecutionCompletion{}, access.ErrNotExecutable
	}
	payload, err := s.payloads.Open(org, id, sealed)
	if err != nil {
		return access.ExecutionCompletion{}, access.ErrPayloadIntegrity
	}
	canonical, err := access.CanonicalPayload(access.ApprovalUnit{OrganizationID: r.OrganizationID, RequesterID: r.RequesterID, ConnectionID: r.ConnectionID, ConnectionConfigVersion: r.ConnectionConfigVersion, PolicyVersion: r.PolicyVersion, Class: r.Class, Title: r.Title, Body: payload.Body, SQL: payload.SQL, Params: payload.Params})
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	verified, err := s.payloads.Verify(canonical, r.Digest, r.DigestKeyVersion)
	if err != nil || !verified {
		return access.ExecutionCompletion{}, access.ErrPayloadIntegrity
	}
	target, err := s.requests.CurrentTarget(ctx, org, r.ConnectionID)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	material, credential, err := s.connections.TestMaterial(ctx, org, r.ConnectionID)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	if target.DBType != r.ConnectionDBType || string(material.DBType) != r.ConnectionDBType {
		return access.ExecutionCompletion{}, access.ErrNotExecutable
	}
	dialect, err := s.dialects.ExecutionDialect(connection.DBType(r.ConnectionDBType))
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	if dialect == nil {
		return access.ExecutionCompletion{}, connection.ErrUnsupportedDBType
	}
	boundSQL, args, err := dialect.BindNamed(payload.SQL, payload.Params)
	if err != nil {
		return access.ExecutionCompletion{}, access.ErrPayloadIntegrity
	}
	parsed, err := dialect.ParseSingle(boundSQL)
	if err != nil {
		return access.ExecutionCompletion{}, access.ErrUnclassifiable
	}
	class, err := dialect.Classify(parsed)
	if err != nil || connection.StatementClass(class) != r.Class {
		return access.ExecutionCompletion{}, access.ErrUnclassifiable
	}
	if material.ConfigVersion != r.ConnectionConfigVersion || target.ConfigVersion != r.ConnectionConfigVersion || target.Policy.Version != r.PolicyVersion {
		return access.ExecutionCompletion{}, access.ErrNotExecutable
	}
	if !target.Policy.Rule(r.Class).Allowed {
		return access.ExecutionCompletion{}, access.ErrClassNotAllowed
	}
	openedCredential, err := s.credentials.Open(org, r.ConnectionID, credential)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	event := auditevent.NewUser(ctx, requester, audit.ActionExecutionStarted, audit.TargetTypeAccessRequest, audit.OutcomeSucceeded)
	targetOutcome := TargetNotAttempted
	if s.admission != nil {
		done, err := s.admission.Allow(r.ConnectionID)
		if err != nil {
			return access.ExecutionCompletion{}, err
		}
		defer func() { done(targetOutcome) }()
	}
	lease, err := s.leases.AcquireExecution(ctx, org, id, requester, s.owner, uuid.NewString(), r.Digest, event)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	leased = true
	started := time.Now()
	workCtx, cancel := context.WithTimeout(ctx, time.Duration(target.Policy.Limits.QueryTimeoutSeconds)*time.Second+query.ExecutionDeadlineGrace)
	defer cancel()
	s.mu.Lock()
	s.cancellations[id] = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.cancellations, id); s.mu.Unlock() }()
	heartbeatDone := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	go s.maintainLease(workCtx, org, lease, cancel, heartbeatDone, heartbeatStopped)
	defer func() { cancel(); close(heartbeatDone); <-heartbeatStopped }()
	targetOutcome = TargetUnhealthy
	completion, targetOutcome = s.runTarget(workCtx, dialect, r, material, openedCredential, query.Execution{SQL: boundSQL, Args: args, Class: class, Governed: true, MaxRows: target.Policy.Limits.MaxRows, MaxResultBytes: min(target.Policy.Limits.MaxResultBytes, query.MaxSnapshotBytes), TimeoutSeconds: target.Policy.Limits.QueryTimeoutSeconds})
	completion.DurationMilliseconds = time.Since(started).Milliseconds()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer finishCancel()
	if err := s.leases.CompleteExecution(finishCtx, org, lease, completion, event); err != nil {
		return access.ExecutionCompletion{}, err
	}
	return completion, nil
}

func (s *Service) runTarget(ctx context.Context, dialect Dialect, r access.Request, material connection.Connection, credential connection.Credential, request query.Execution) (access.ExecutionCompletion, TargetOutcome) {
	completion := access.ExecutionCompletion{State: access.StateOutcomeUnknown}
	stream, err := dialect.Execute(ctx, material.Target, material.TLSMode, credential, request)
	if err != nil {
		completion.State = failedExecutionState(err)
		return completion, targetHealthAfterFailure(err)
	}
	defer func() { _ = stream.Close() }()
	rows := make([][]query.CellValue, 0, min(request.MaxRows, 100))
	for stream.Next() {
		rows = append(rows, append([]query.CellValue(nil), stream.Row()...))
	}
	if err := stream.Err(); err != nil {
		completion.State = failedExecutionState(err)
		return completion, targetHealthAfterFailure(err)
	}
	completion.State = access.StateSucceeded
	completion.RowsAffected = stream.RowsAffected()
	completion.Truncated = stream.Truncated()
	completion.RowCount = int64(len(rows))
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: r.OrganizationID, OwnerID: r.RequesterID, RowCount: int64(len(rows)), Truncated: completion.Truncated}
	// The statement already committed, so persisting its snapshot must not depend on the execution deadline or a later cancellation.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), resultPersistTimeout)
	defer persistCancel()
	stored, err := s.results.Save(persistCtx, metadata, stream.Columns(), rows, request.MaxResultBytes)
	if err != nil {
		completion.ResultUnavailableReason = resultUnavailableReason(err)
		return completion, TargetHealthy
	}
	completion.ResultID = stored.ID
	completion.ResultExpiresAt = &stored.ExpiresAt
	completion.RowCount = stored.RowCount
	completion.ByteCount = stored.ByteCount
	completion.Truncated = stored.Truncated
	return completion, TargetHealthy
}

// resultPersistTimeout bounds snapshot persistence after the target statement has committed.
const resultPersistTimeout = 10 * time.Second

// resultUnavailableReason classifies a snapshot persistence failure without exposing its error text.
func resultUnavailableReason(err error) access.ResultUnavailableReason {
	if errors.Is(err, query.ErrResultStoreFull) {
		return access.ResultUnavailableStoreFull
	}
	return access.ResultUnavailablePersistenceFailed
}

// queryCanceledSQLState is PostgreSQL's query_canceled, raised when the server's statement timeout or an administrator cancels a statement.
const queryCanceledSQLState = "57014"

// targetHealthAfterFailure separates target availability from cancellations and timeouts, which say nothing about it (ADR-0010).
func targetHealthAfterFailure(err error) TargetOutcome {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, query.ErrResponseLimit) || isServerCancellation(err) {
		return TargetInconclusive
	}
	var connectionFailure *connection.TestError
	if errors.As(err, &connectionFailure) || failedExecutionState(err) == access.StateOutcomeUnknown {
		return TargetUnhealthy
	}
	return TargetHealthy
}

// isServerCancellation reports whether the target itself cancelled the statement and rolled it back.
func isServerCancellation(err error) bool {
	var sqlFailure *query.ExecError
	return errors.As(err, &sqlFailure) && sqlFailure.SQLState == queryCanceledSQLState
}

// failedExecutionState maps a target failure to its terminal state: a certain rollback is failed or cancelled, anything unconfirmed is outcome_unknown (PRD §4.4).
func failedExecutionState(err error) access.State {
	if errors.Is(err, query.ErrInterruptedBeforeCommit) {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return access.StateFailed
		case errors.Is(err, context.Canceled):
			return access.StateCancelled
		}
		return access.StateOutcomeUnknown
	}
	var rejection *query.Rejection
	var parseFailure *query.ParseFailure
	var dialFailure *connection.TestError
	var sqlFailure *query.ExecError
	if errors.As(err, &rejection) || errors.As(err, &parseFailure) || errors.As(err, &dialFailure) {
		return access.StateFailed
	}
	if errors.As(err, &sqlFailure) && len(sqlFailure.SQLState) == 5 && sqlFailure.SQLState[:2] != "08" && sqlFailure.SQLState != "40003" {
		return access.StateFailed
	}
	return access.StateOutcomeUnknown
}

func (s *Service) maintainLease(ctx context.Context, org identity.OrganizationID, lease access.ExecutionLease, cancel context.CancelFunc, done <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	ticker := time.NewTicker(access.ExecutionHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			heartbeatCtx, heartbeatCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err := s.leases.HeartbeatExecution(heartbeatCtx, org, lease)
			heartbeatCancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

// Cancel interrupts an active execution owned by the authenticated requester.
func (s *Service) Cancel(ctx context.Context, requester identity.UserID, id access.RequestID) error {
	org, err := s.requests.DefaultOrganizationID(ctx)
	if err != nil {
		return err
	}
	r, _, err := s.requests.GetSealed(ctx, org, id)
	if err != nil {
		return err
	}
	if r.RequesterID != requester {
		return access.ErrNotFound
	}
	s.mu.Lock()
	cancel := s.cancellations[id]
	s.mu.Unlock()
	if cancel == nil {
		return access.ErrNotExecutable
	}
	event := auditevent.NewUser(ctx, requester, audit.Action("EXECUTION_CANCEL_REQUESTED"), audit.TargetTypeAccessRequest, audit.OutcomeSucceeded)
	if err := s.leases.RecordExecutionRefusal(ctx, org, id, requester, "cancel_requested", event); err != nil {
		return err
	}
	cancel()
	return nil
}

// Get returns durable execution metadata only to its original requester.
func (s *Service) Get(ctx context.Context, requester identity.UserID, id access.RequestID) (access.ExecutionCompletion, error) {
	org, err := s.requests.DefaultOrganizationID(ctx)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	r, _, err := s.requests.GetSealed(ctx, org, id)
	if err != nil {
		return access.ExecutionCompletion{}, err
	}
	if r.RequesterID != requester {
		return access.ExecutionCompletion{}, access.ErrNotFound
	}
	return s.leases.GetExecution(ctx, org, id)
}

// ResultOwner resolves an owner-scoped cached result handle without accepting a client handle.
func (s *Service) ResultOwner(ctx context.Context, requester identity.UserID, id access.RequestID) (identity.OrganizationID, string, error) {
	completion, err := s.Get(ctx, requester, id)
	if err != nil {
		return "", "", err
	}
	if completion.State != access.StateSucceeded || completion.ResultID == "" || completion.ResultExpiresAt == nil || !time.Now().Before(*completion.ResultExpiresAt) {
		return "", "", query.ErrResultUnavailable
	}
	org, err := s.requests.DefaultOrganizationID(ctx)
	return org, completion.ResultID, err
}

// StopAdmission refuses new executions while the server drains active work.
func (s *Service) StopAdmission() { s.mu.Lock(); s.draining = true; s.mu.Unlock() }

// Reconcile recovers expired execution owners in the self-hosted organization, paging past failing or locked attempts while a full batch is listed; only a batch that cannot list attempts fails the run.
func (s *Service) Reconcile(ctx context.Context) (access.ReconcileSummary, error) {
	var summary access.ReconcileSummary
	org, err := s.requests.DefaultOrganizationID(ctx)
	if err != nil {
		return summary, err
	}
	var cursor access.ReconcileCursor
	for {
		batch, err := s.leases.ReconcileExecutions(ctx, org, cursor, access.ExecutionReconcileBatchSize)
		summary.Recovered += batch.Recovered
		summary.Skipped += batch.Skipped
		summary.Failed += batch.Failed
		if summary.FirstFailure == nil {
			summary.FirstFailure = batch.FirstFailure
		}
		if err != nil {
			return summary, err
		}
		cursor = batch.Next
		if batch.Listed < access.ExecutionReconcileBatchSize {
			return summary, nil
		}
		if err := ctx.Err(); err != nil {
			return summary, err
		}
	}
}

// WithAdmission installs the target circuit breaker before lease acquisition.
func (s *Service) WithAdmission(admission Admission) *Service { s.admission = admission; return s }
