package accessrequest_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/accessrequest"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

var (
	t0        = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	requester = identity.UserID("user-requester")
	approver1 = identity.UserID("user-approver-1")
	approver2 = identity.UserID("user-approver-2")
	connRead  = connection.ConnectionID("conn-read")
	connAuto  = connection.ConnectionID("conn-auto")
	connWrite = connection.ConnectionID("conn-write")
	connGone  = connection.ConnectionID("conn-gone")
)

type fakeRepo struct {
	policies map[connection.ConnectionID]connection.Policy

	configVersions map[connection.ConnectionID]int64
	archived       map[connection.ConnectionID]bool
	requests       map[access.RequestID]access.Request
	sealed         map[access.RequestID]access.SealedPayload
	approvals      map[access.RequestID][]access.Approval

	valid  map[identity.UserID]bool
	events []audit.Event
	clock  func() time.Time
	// dbNow overrides the instant the view rows are stamped with, standing in for the DATABASE clock. Nil means "same as the app clock".
	dbNow func() time.Time

	listCalls int

	submitValidity time.Duration
	// submitExpectedVersion records the optimistic token Submit guarded with — it must be the version of the row the pipeline actually read, never a number the client chose.
	submitExpectedVersion int64
	// updateExpectedVersion records the same for UpdateDraft, and updateCalls counts the writes: a refused edit must never reach the store at all.
	updateExpectedVersion int64
	updateCalls           int

	afterGetSealed func()

	policyReadIgnoresArchive bool

	bumpOnPolicyRead bool
}

func newRepo() *fakeRepo {
	pol := func(readQuorum int, writeAllowed bool) connection.Policy {
		return connection.Policy{
			Version: 3,
			Read:    connection.ClassRule{Allowed: true, RequiredApprovals: readQuorum},
			Write:   connection.ClassRule{Allowed: writeAllowed, RequiredApprovals: 1},
			DDL:     connection.ClassRule{Allowed: false, RequiredApprovals: 1},
		}
	}
	return &fakeRepo{
		policies: map[connection.ConnectionID]connection.Policy{
			connRead:  pol(2, false),
			connAuto:  pol(0, false),
			connWrite: pol(1, true),
			connGone:  pol(1, false),
		},
		configVersions: map[connection.ConnectionID]int64{},
		archived:       map[connection.ConnectionID]bool{connGone: true},
		requests:       map[access.RequestID]access.Request{},
		sealed:         map[access.RequestID]access.SealedPayload{},
		approvals:      map[access.RequestID][]access.Approval{},
		valid:          map[identity.UserID]bool{approver1: true, approver2: true},
		clock:          func() time.Time { return t0 },
	}
}

func (f *fakeRepo) DefaultOrganizationID(context.Context) (identity.OrganizationID, error) {
	return "org-default", nil
}

func (f *fakeRepo) ListRequestable(_ context.Context, _ identity.OrganizationID) ([]access.RequestableConnection, error) {
	ids := make([]connection.ConnectionID, 0, len(f.policies))
	for id := range f.policies {
		if !f.archived[id] {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(idx, innerIdx int) bool { return ids[idx] < ids[innerIdx] })
	out := make([]access.RequestableConnection, 0, len(ids))
	for _, id := range ids {
		out = append(out, access.RequestableConnection{
			ID: id, DisplayName: string(id), DBType: "postgresql", Environment: "development",
		})
	}
	return out, nil
}

func (f *fakeRepo) CurrentTarget(_ context.Context, _ identity.OrganizationID, id connection.ConnectionID) (access.SubmitTarget, error) {
	if f.archived[id] && !f.policyReadIgnoresArchive {
		return access.SubmitTarget{}, access.ErrConnectionArchived
	}
	p, ok := f.policies[id]
	if !ok {
		return access.SubmitTarget{}, connection.ErrNotFound
	}
	if f.bumpOnPolicyRead {

		next := p
		next.Version = p.Version + 1
		f.policies[id] = next
	}
	version, ok := f.configVersions[id]
	if !ok {
		version = 1
	}
	return access.SubmitTarget{
		Policy:        p,
		ConfigVersion: version,
		Fingerprint:   "v1|sha256:" + string(id),
		DisplayName:   "Connection " + string(id),
		DBType:        "postgresql",
	}, nil
}

// CreateDraft mirrors the real adapter: the archived check happens HERE, inside the write (the store locks the connection row), so a concurrent archive cannot slip past a caller's pre-read (ADR-0018).
func (f *fakeRepo) CreateDraft(_ context.Context, r access.Request, sealed access.SealedPayload, events ...audit.Event) (access.RequestView, error) {
	if f.archived[r.ConnectionID] {
		return access.RequestView{}, access.ErrConnectionArchived
	}
	if _, ok := f.policies[r.ConnectionID]; !ok {
		return access.RequestView{}, connection.ErrNotFound
	}
	f.requests[r.ID] = r
	f.sealed[r.ID] = sealed
	f.events = append(f.events, events...)
	return f.view(r), nil
}

func (f *fakeRepo) GetSealed(_ context.Context, _ identity.OrganizationID, id access.RequestID) (access.Request, access.SealedPayload, error) {
	r, ok := f.requests[id]
	if !ok {
		return access.Request{}, access.SealedPayload{}, access.ErrNotFound
	}
	payload := f.sealed[id]
	// The caller now holds this version's payload. Anything that happens next — including a concurrent edit — must not be able to redirect the snapshot this read is about to produce (see afterGetSealed).
	if f.afterGetSealed != nil {
		f.afterGetSealed()
	}
	return r, payload, nil
}

func (f *fakeRepo) UpdateDraft(_ context.Context, r access.Request, sealed access.SealedPayload, expectedVersion int64, events ...audit.Event) (access.RequestView, error) {
	f.updateExpectedVersion = expectedVersion
	f.updateCalls++
	cur, ok := f.requests[r.ID]
	if !ok {
		return access.RequestView{}, access.ErrNotFound
	}
	if cur.State != access.StateDraft {
		return access.RequestView{}, access.ErrNotDraft
	}
	if cur.Version != expectedVersion {
		return access.RequestView{}, access.ErrConflict
	}
	cur.Version++
	f.requests[r.ID] = cur
	f.sealed[r.ID] = sealed
	f.events = append(f.events, events...)
	return f.view(cur), nil
}

// The fake mirrors under-lock target/policy checks and stamps automatic approval expiry at the transition.
func (f *fakeRepo) Submit(_ context.Context, r access.Request, expectedVersion int64, validity time.Duration, events ...audit.Event) (access.RequestView, error) {
	f.submitValidity = validity
	f.submitExpectedVersion = expectedVersion
	cur, ok := f.requests[r.ID]
	if !ok {
		return access.RequestView{}, access.ErrNotFound
	}
	if f.archived[r.ConnectionID] {
		return access.RequestView{}, access.ErrConnectionArchived
	}
	if pol, ok := f.policies[r.ConnectionID]; ok && pol.Version != r.PolicyVersion {
		return access.RequestView{}, connection.ErrPolicyConflict
	}
	if cur.State != access.StateDraft {
		return access.RequestView{}, access.ErrNotDraft
	}
	if cur.Version != expectedVersion {
		return access.RequestView{}, access.ErrConflict
	}
	r.Version = cur.Version + 1
	if r.State == access.StateApproved {
		expires := f.clock().Add(validity)
		r.ExpiresAt = &expires
	}
	f.requests[r.ID] = r
	f.events = append(f.events, events...)
	return f.view(r), nil
}

func (f *fakeRepo) countValid(id access.RequestID) int {
	n := 0
	for _, a := range f.approvals[id] {
		if a.Decision == access.DecisionApproved && f.valid[a.ApproverID] {
			n++
		}
	}
	return n
}

func (f *fakeRepo) decide(id access.RequestID, approver identity.UserID, decision access.Decision, reason string, validity time.Duration) (access.Request, error) {
	r, ok := f.requests[id]
	if !ok {
		return access.Request{}, access.ErrNotFound
	}

	if r.State == access.StateApproved && r.ExpiresAt != nil && r.ExpiresAt.Before(f.clock()) {
		r.State = access.StateExpired
		r.Reason = access.ReasonTTLExpired
		f.requests[id] = r
	}
	if r.State != access.StatePending {
		return access.Request{}, access.ErrNotPending
	}
	a, err := access.NewApproval(id, r.OrganizationID, approver, r.RequesterID, decision, reason, f.clock())
	if err != nil {
		return access.Request{}, err
	}
	if !f.valid[approver] {
		return access.Request{}, access.ErrInvalidApproval
	}
	for _, prev := range f.approvals[id] {
		if prev.ApproverID == approver {
			return access.Request{}, access.ErrAlreadyDecided
		}
	}
	f.approvals[id] = append(f.approvals[id], a)
	switch decision {
	case access.DecisionRejected:
		r.State = access.StateRejected
	case access.DecisionApproved:
		if f.countValid(id) >= r.RequiredApprovals {
			if r, err = r.Approved(f.clock(), validity); err != nil {
				return access.Request{}, err
			}
		}
	}
	f.requests[id] = r
	return r, nil
}

func (f *fakeRepo) Approve(_ context.Context, _ identity.OrganizationID, id access.RequestID, approver identity.UserID, reason string, validity time.Duration, evt audit.Event) (access.RequestView, error) {
	r, err := f.decide(id, approver, access.DecisionApproved, reason, validity)
	if err != nil {
		return access.RequestView{}, err
	}
	f.events = append(f.events, evt)
	return f.view(r), nil
}

func (f *fakeRepo) Reject(_ context.Context, _ identity.OrganizationID, id access.RequestID, approver identity.UserID, reason string, evt audit.Event) (access.RequestView, error) {
	r, err := f.decide(id, approver, access.DecisionRejected, reason, 0)
	if err != nil {
		return access.RequestView{}, err
	}
	f.events = append(f.events, evt)
	return f.view(r), nil
}

func (f *fakeRepo) Cancel(_ context.Context, _ identity.OrganizationID, id access.RequestID, requesterID identity.UserID, evt audit.Event) (access.RequestView, error) {
	r, ok := f.requests[id]
	if !ok || r.RequesterID != requesterID {
		return access.RequestView{}, access.ErrNotFound
	}
	if err := r.ValidateCancellation(); err != nil {
		return access.RequestView{}, err
	}
	r.State = access.StateCancelled
	f.requests[id] = r
	f.events = append(f.events, evt)
	return f.view(r), nil
}

func (f *fakeRepo) Get(_ context.Context, _ identity.OrganizationID, id access.RequestID) (access.RequestView, access.SealedPayload, error) {
	r, ok := f.requests[id]
	if !ok {
		return access.RequestView{}, access.SealedPayload{}, access.ErrNotFound
	}
	return f.view(r), f.sealed[id], nil
}

func (f *fakeRepo) List(_ context.Context, _ identity.OrganizationID, q access.ListQuery) (access.RequestPage, error) {
	f.listCalls++
	var items []access.RequestView
	for _, r := range f.requests {
		if q.RequesterID != "" && r.RequesterID != q.RequesterID {
			continue
		}
		if q.State != "" && r.State != q.State {
			continue
		}
		items = append(items, f.view(r))
	}
	total := int64(len(items))
	page := q.Page
	if last := (len(items) + q.PageSize - 1) / q.PageSize; last > 0 && page > last {
		page = last
	} else if last == 0 {
		page = 1
	}
	offset := min((page-1)*q.PageSize, len(items))
	return access.RequestPage{
		Items:      items[offset:min(offset+q.PageSize, len(items))],
		Page:       page,
		TotalCount: total,
	}, nil
}

func (f *fakeRepo) view(r access.Request) access.RequestView {
	views := make([]access.ApprovalView, 0, len(f.approvals[r.ID]))
	for _, a := range f.approvals[r.ID] {
		views = append(views, access.ApprovalView{Approval: a, Valid: f.valid[a.ApproverID]})
	}
	v := access.RequestView{
		Request:        r,
		Approvals:      views,
		ValidApprovals: f.countValid(r.ID),
	}
	// The real store's view queries compute the effective state in SQL, in the same statement (and from the same clock) that filters and counts on it — so the fake carries it too, or the service's contract goes untested.
	v.StampEffective(f.viewClock())
	return v
}

// viewClock is the DB's clock as far as a view row is concerned. Tests move it away from the service's clock to prove which one the response reflects.
func (f *fakeRepo) viewClock() time.Time {
	if f.dbNow != nil {
		return f.dbNow()
	}
	return f.clock()
}

type fakeCodec struct {
	digestKV uint32
}

func (c fakeCodec) Seal(_ identity.OrganizationID, id access.RequestID, p access.Payload) (access.SealedPayload, error) {
	if id == "" {
		return access.SealedPayload{}, errors.New("seal without id (AAD binding broken)")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return access.SealedPayload{}, err
	}
	return access.SealedPayload{KeyVersion: 1, WrappedDEK: []byte("dek"), Nonce: []byte("n"), Ciphertext: b}, nil
}

func (c fakeCodec) Open(_ identity.OrganizationID, _ access.RequestID, sealed access.SealedPayload) (access.Payload, error) {
	var p access.Payload
	if err := json.Unmarshal(sealed.Ciphertext, &p); err != nil {
		return access.Payload{}, err
	}
	return p, nil
}

func (c fakeCodec) Digest(canonical []byte) ([]byte, uint32, error) {
	return append([]byte("digest:"), canonical[:min(8, len(canonical))]...), c.digestKV, nil
}

type fakeStmt struct{ text string }

func (s fakeStmt) Text() string { return s.text }

type fakeDialect struct{}

func (fakeDialect) ParseSingle(sql string) (query.Statement, error) {
	if strings.Contains(sql, ";") {
		return nil, query.ErrMultipleStatements
	}
	return fakeStmt{text: sql}, nil
}

func (fakeDialect) Classify(st query.Statement) (query.StatementClass, error) {
	switch {
	case strings.HasPrefix(st.Text(), "select"):
		return query.ClassRead, nil
	case strings.HasPrefix(st.Text(), "update"):
		return query.ClassWrite, nil
	case strings.HasPrefix(st.Text(), "create"):
		return query.ClassDDL, nil
	}
	return "", errors.New("unclassifiable statement")
}

func (fakeDialect) BindNamed(sql string, params []query.Parameter) (string, []query.TypedValue, error) {
	for _, p := range params {
		if !strings.Contains(sql, ":"+p.Name) {
			return "", nil, query.ErrUnusedParameter
		}
	}
	return sql, nil, nil
}

func (fakeDialect) Redact(st query.Statement) (query.Redaction, error) {
	if strings.Contains(st.Text(), "unredactable") {
		return query.Redaction{}, errors.New("redaction failed")
	}
	return query.Redaction{SQL: "<redacted> " + st.Text()[:min(6, len(st.Text()))]}, nil
}

func newService(t *testing.T, repo *fakeRepo) *accessrequest.Service {
	t.Helper()
	svc, err := accessrequest.New(repo, repo, fakeCodec{digestKV: 1}, fakeDialect{}, 24*time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seq := 0
	return svc.WithClock(func() time.Time { return t0 }).WithIDGenerator(func() string {
		seq++
		return "req-" + string(rune('0'+seq))
	})
}

func createDraft(t *testing.T, svc *accessrequest.Service, connID connection.ConnectionID, sql string, params ...query.Parameter) access.Request {
	t.Helper()
	v, err := svc.Create(context.Background(), requester, accessrequest.CreateParams{ConnectionID: connID, SQL: sql, Params: params})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return v.Request
}

func TestCreateDraft(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	r := createDraft(t, svc, connRead, "select 1 where x = :id", query.Parameter{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "7"}})
	if r.State != access.StateDraft || r.RequesterID != requester || r.ConnectionID != connRead {
		t.Errorf("draft shape: %+v", r)
	}
	if _, ok := repo.sealed[r.ID]; !ok {
		t.Error("payload was not sealed/stored")
	}
	if len(repo.events) != 1 || repo.events[0].Action != audit.ActionAccessRequestCreated {
		t.Errorf("events = %+v, want one CREATED", repo.events)
	}
	if repo.events[0].ConnectionID != string(connRead) || repo.events[0].TargetID != string(r.ID) {
		t.Errorf("CREATED snapshot fields wrong: %+v", repo.events[0])
	}
}

func TestCreateRefusals(t *testing.T) {
	t.Parallel()
	svc := newService(t, newRepo())
	ctx := context.Background()
	if _, err := svc.Create(ctx, requester, accessrequest.CreateParams{ConnectionID: connGone, SQL: "select 1"}); !errors.Is(err, access.ErrConnectionArchived) {
		t.Errorf("archived = %v, want ErrConnectionArchived", err)
	}
	if _, err := svc.Create(ctx, requester, accessrequest.CreateParams{ConnectionID: "conn-ghost", SQL: "select 1"}); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("unknown connection = %v, want connection.ErrNotFound", err)
	}
	if _, err := svc.Create(ctx, requester, accessrequest.CreateParams{ConnectionID: connRead, SQL: "   "}); !errors.Is(err, access.ErrInvalidPayload) {
		t.Errorf("blank sql = %v, want ErrInvalidPayload", err)
	}
}

func TestUpdateDraft(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()
	r := createDraft(t, svc, connRead, "select 1")

	updated, err := svc.UpdateDraft(ctx, requester, r.ID, accessrequest.UpdateDraftParams{SQL: "select 2", ExpectedVersion: 1})
	if err != nil {
		t.Fatalf("UpdateDraft: %v", err)
	}
	if updated.Request.Version != 2 {
		t.Errorf("version = %d, want 2", updated.Request.Version)
	}

	if _, err := svc.UpdateDraft(ctx, approver1, r.ID, accessrequest.UpdateDraftParams{SQL: "select 3", ExpectedVersion: 2}); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("foreign edit = %v, want ErrNotFound", err)
	}

	if _, err := svc.UpdateDraft(ctx, requester, r.ID, accessrequest.UpdateDraftParams{SQL: "select 3", ExpectedVersion: 1}); !errors.Is(err, access.ErrConflict) {
		t.Errorf("stale version = %v, want ErrConflict", err)
	}

	if _, err := svc.Submit(ctx, requester, r.ID, 2); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := svc.UpdateDraft(ctx, requester, r.ID, accessrequest.UpdateDraftParams{SQL: "select 4", ExpectedVersion: 3}); !errors.Is(err, access.ErrNotDraft) {
		t.Errorf("post-submit edit = %v, want ErrNotDraft", err)
	}
}

func TestSubmitPinsSnapshot(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	r := createDraft(t, svc, connRead, "select x from t where id = :id", query.Parameter{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "1"}})

	submitted, err := svc.Submit(context.Background(), requester, r.ID, 1)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if submitted.Request.State != access.StatePending {
		t.Errorf("state = %q, want pending", submitted.Request.State)
	}
	if submitted.Request.Class != connection.ClassRead || submitted.Request.PolicyVersion != 3 || submitted.Request.RequiredApprovals != 2 {
		t.Errorf("pin wrong: %+v", submitted)
	}
	if len(submitted.Request.Digest) == 0 || submitted.Request.DigestKeyVersion != 1 || !strings.HasPrefix(submitted.Request.RedactedSQL, "<redacted>") {
		t.Errorf("digest/redaction missing: %+v", submitted)
	}
	last := repo.events[len(repo.events)-1]
	if last.Action != audit.ActionAccessRequestSubmitted {
		t.Fatalf("last event = %s, want SUBMITTED", last.Action)
	}
	if last.Metadata["redacted_sql"] != submitted.Request.RedactedSQL || last.QueryType != "read" ||
		string(last.PayloadDigest) == "" || last.PreviousState != "draft" || last.NextState != "pending" {
		t.Errorf("SUBMITTED event incomplete: %+v", last)
	}

	for field, want := range map[string]any{
		"connection_display_name":   submitted.Request.ConnectionDisplayName,
		"connection_db_type":        submitted.Request.ConnectionDBType,
		"connection_fingerprint":    submitted.Request.ConnectionFingerprint,
		"connection_config_version": submitted.Request.ConnectionConfigVersion,
	} {
		if last.Metadata[field] != want {
			t.Errorf("SUBMITTED metadata %s = %v, want the row's %v", field, last.Metadata[field], want)
		}
		if want == "" || want == int64(0) {
			t.Errorf("the row itself has no %s — the snapshot never reached it", field)
		}
	}
}

func TestSubmitGuardsTheVersionItRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("guards with the version the pipeline read", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")
		if _, err := svc.Submit(ctx, requester, r.ID, r.Version); err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if repo.submitExpectedVersion != r.Version {
			t.Errorf("store guarded with version %d, want the read row's %d",
				repo.submitExpectedVersion, r.Version)
		}
	})

	t.Run("refuses a version the row does not have", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")

		if _, err := svc.Submit(ctx, requester, r.ID, r.Version+1); !errors.Is(err, access.ErrConflict) {
			t.Errorf("submit with a future version = %v, want ErrConflict", err)
		}

		if _, err := svc.Submit(ctx, requester, r.ID, r.Version-1); !errors.Is(err, access.ErrConflict) {
			t.Errorf("submit with a stale version = %v, want ErrConflict", err)
		}

		stored := repo.requests[r.ID]
		if stored.State != access.StateDraft || stored.Class != "" || len(stored.Digest) != 0 {
			t.Errorf("refused submit left a snapshot on the row: %+v", stored)
		}
	})

	t.Run("adversarial: an edit landing at the named version cannot hijack the snapshot", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")
		hijacked := "select 'B'"

		repo.afterGetSealed = func() {
			repo.afterGetSealed = nil
			edited := repo.requests[r.ID]
			edited.Version = r.Version + 1
			repo.requests[r.ID] = edited
			repo.sealed[r.ID] = access.SealedPayload{
				KeyVersion: 1, WrappedDEK: []byte("dek"), Nonce: []byte("nonce"),
				Ciphertext: []byte(`{"SQL":"` + hijacked + `","Params":[]}`),
			}
		}

		_, err := svc.Submit(ctx, requester, r.ID, r.Version+1)
		if !errors.Is(err, access.ErrConflict) {
			t.Errorf("submit naming a version the pipeline never read = %v, want ErrConflict", err)
		}
		// The row must not be carrying payload A's snapshot over payload B: the approvers would review a statement the request no longer holds.
		stored := repo.requests[r.ID]
		if stored.State != access.StateDraft || stored.RedactedSQL != "" || len(stored.Digest) != 0 {
			t.Errorf("a hijacked submit left a snapshot behind: state=%s redacted=%q digest=%d bytes",
				stored.State, stored.RedactedSQL, len(stored.Digest))
		}
	})

	t.Run("an edit before submit supersedes the old version", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")

		if _, err := svc.UpdateDraft(ctx, requester, r.ID, accessrequest.UpdateDraftParams{
			SQL: "select 2", ExpectedVersion: r.Version,
		}); err != nil {
			t.Fatalf("UpdateDraft: %v", err)
		}
		// Submitting with the ORIGINAL version must now fail: that row is gone.
		if _, err := svc.Submit(ctx, requester, r.ID, r.Version); !errors.Is(err, access.ErrConflict) {
			t.Errorf("submit against the superseded version = %v, want ErrConflict", err)
		}

		current := repo.requests[r.ID]
		view, err := svc.Submit(ctx, requester, r.ID, current.Version)
		if err != nil {
			t.Fatalf("Submit current: %v", err)
		}
		if view.Request.RedactedSQL == "" || repo.submitExpectedVersion != current.Version {
			t.Errorf("submit snapshot = %q guarded with %d, want payload B at version %d",
				view.Request.RedactedSQL, repo.submitExpectedVersion, current.Version)
		}
	})
}

func TestUpdateDraftGuardsTheVersionItRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	edit := func(version int64, sql string) accessrequest.UpdateDraftParams {
		return accessrequest.UpdateDraftParams{SQL: sql, ExpectedVersion: version}
	}

	t.Run("guards with the version it read", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")
		if _, err := svc.UpdateDraft(ctx, requester, r.ID, edit(r.Version, "select 2")); err != nil {
			t.Fatalf("UpdateDraft: %v", err)
		}
		if repo.updateExpectedVersion != r.Version {
			t.Errorf("store guarded with version %d, want the read row's %d", repo.updateExpectedVersion, r.Version)
		}
	})

	t.Run("refuses a version the row does not have, without touching the store", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")

		for _, tc := range []struct {
			name    string
			version int64
		}{
			{"a future token names a row that does not exist yet", r.Version + 1},
			{"a far-future token", r.Version + 99},
			{"a stale token was already superseded", r.Version - 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := svc.UpdateDraft(ctx, requester, r.ID, edit(tc.version, "select 'X'")); !errors.Is(err, access.ErrConflict) {
					t.Errorf("edit with version %d = %v, want ErrConflict", tc.version, err)
				}
			})
		}

		if repo.updateCalls != 0 {
			t.Errorf("refused edits reached the store %d times, want 0", repo.updateCalls)
		}
		if repo.requests[r.ID].Version != r.Version {
			t.Errorf("row version = %d, want the untouched %d", repo.requests[r.ID].Version, r.Version)
		}
	})

	t.Run("adversarial: an edit landing at the named version is not overwritten", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")
		concurrent := access.SealedPayload{
			KeyVersion: 1, WrappedDEK: []byte("dek"), Nonce: []byte("nonce"),
			Ciphertext: []byte(`{"SQL":"select 'B'","Params":[]}`),
		}

		repo.afterGetSealed = func() {
			repo.afterGetSealed = nil
			landed := repo.requests[r.ID]
			landed.Version = r.Version + 1
			repo.requests[r.ID] = landed
			repo.sealed[r.ID] = concurrent
		}

		if _, err := svc.UpdateDraft(ctx, requester, r.ID, edit(r.Version+1, "select 'A'")); !errors.Is(err, access.ErrConflict) {
			t.Errorf("edit naming a version it never read = %v, want ErrConflict", err)
		}
		if got := string(repo.sealed[r.ID].Ciphertext); got != string(concurrent.Ciphertext) {
			t.Errorf("payload = %s, want the concurrent edit's %s — a version the caller never read was overwritten",
				got, concurrent.Ciphertext)
		}
	})

	t.Run("sequential edits keep working", func(t *testing.T) {
		t.Parallel()
		repo := newRepo()
		svc := newService(t, repo)
		r := createDraft(t, svc, connAuto, "select 1")

		second, err := svc.UpdateDraft(ctx, requester, r.ID, edit(r.Version, "select 2"))
		if err != nil {
			t.Fatalf("first edit: %v", err)
		}
		third, err := svc.UpdateDraft(ctx, requester, r.ID, edit(second.Request.Version, "select 3"))
		if err != nil {
			t.Fatalf("second edit: %v", err)
		}
		if third.Request.Version != second.Request.Version+1 || repo.updateExpectedVersion != second.Request.Version {
			t.Errorf("version walk = %d then guard %d, want %d then %d",
				third.Request.Version, repo.updateExpectedVersion, second.Request.Version+1, second.Request.Version)
		}
	})
}

func TestSubmitAutoApproval(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	r := createDraft(t, svc, connAuto, "select 1")

	final, err := svc.Submit(context.Background(), requester, r.ID, 1)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if final.Request.State != access.StateApproved {
		t.Errorf("state = %q, want approved", final.Request.State)
	}
	if final.Request.ExpiresAt == nil || !final.Request.ExpiresAt.Equal(t0.Add(24*time.Hour)) {
		t.Errorf("ExpiresAt = %v, want t0+24h", final.Request.ExpiresAt)
	}
	// The window is HANDED to the store rather than only precomputed here: the store stamps it once it holds the connection row lock, so a lock wait cannot spend the validity before the approval lands (PRD §4.3 dates it from the moment the auto-approval occurs).
	if repo.submitValidity != 24*time.Hour {
		t.Errorf("validity passed to Submit = %s, want the configured 24h", repo.submitValidity)
	}

	n := len(repo.events)
	sub, appr := repo.events[n-2], repo.events[n-1]
	if sub.Action != audit.ActionAccessRequestSubmitted || appr.Action != audit.ActionAccessRequestApproved {
		t.Fatalf("event pair = %s, %s", sub.Action, appr.Action)
	}
	if appr.ActorType != audit.ActorSystem || appr.ActorService != access.ActorAutoApproval || appr.ActorUserID != nil {
		t.Errorf("auto-approval actor wrong: %+v", appr)
	}
	if len(repo.approvals[r.ID]) != 0 {
		t.Error("auto-approval must not create approvals rows (§4.4)")
	}
	// Every event from SUBMITTED onward carries a COPY of the redacted SQL so each audit row is self-contained (§6.1, ADR-0018) — the system APPROVED event included; it must not depend on reading its sibling SUBMITTED row.
	if appr.Metadata["redacted_sql"] != final.Request.RedactedSQL {
		t.Errorf("auto-approval metadata redacted_sql = %v, want the request's %q", appr.Metadata["redacted_sql"], final.Request.RedactedSQL)
	}
}

func TestSubmitRefusals(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()

	cases := []struct {
		name string
		conn connection.ConnectionID
		sql  string
		want error
	}{
		{"disallowed class", connRead, "update t set x = 1", access.ErrClassNotAllowed},
		{"unclassifiable", connRead, "boom please", access.ErrUnclassifiable},
		{"multi statement", connRead, "select 1; select 2", access.ErrUnclassifiable},
		{"unredactable", connRead, "select unredactable", access.ErrInvalidPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := createDraft(t, svc, tc.conn, tc.sql)
			if _, err := svc.Submit(ctx, requester, r.ID, 1); !errors.Is(err, tc.want) {
				t.Errorf("Submit = %v, want %v", err, tc.want)
			}
		})
	}

	r := createDraft(t, svc, connRead, "select 1", query.Parameter{Name: "ghost", Value: query.TypedValue{Type: query.ParamNull}})
	if _, err := svc.Submit(ctx, requester, r.ID, 1); !errors.Is(err, access.ErrInvalidPayload) {
		t.Errorf("unused param = %v, want ErrInvalidPayload", err)
	}

	rw := createDraft(t, svc, connWrite, "update t set x = 1")
	if got, err := svc.Submit(ctx, requester, rw.ID, 1); err != nil || got.Request.Class != connection.ClassWrite {
		t.Errorf("allowed write = %+v, %v", got, err)
	}
}

func TestApproveQuorum(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()
	r := createDraft(t, svc, connRead, "select 1")
	if _, err := svc.Submit(ctx, requester, r.ID, 1); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	view, err := svc.Approve(ctx, approver1, r.ID, "")
	if err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if view.Request.State != access.StatePending || view.ValidApprovals != 1 {
		t.Errorf("after 1/2: state=%q valid=%d", view.Request.State, view.ValidApprovals)
	}

	view, err = svc.Approve(ctx, approver2, r.ID, "lgtm")
	if err != nil {
		t.Fatalf("second approve: %v", err)
	}
	if view.Request.State != access.StateApproved || view.Request.ExpiresAt == nil {
		t.Errorf("after 2/2: %+v", view.Request)
	}
	if view.EffectiveState != access.StateApproved {
		t.Errorf("effective = %q", view.EffectiveState)
	}

	if _, err := svc.Approve(ctx, approver1, r.ID, ""); !errors.Is(err, access.ErrNotPending) {

		t.Errorf("post-approval approve = %v, want ErrNotPending", err)
	}
}

func TestApproveRefusals(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()
	r := createDraft(t, svc, connRead, "select 1")
	if _, err := svc.Submit(ctx, requester, r.ID, 1); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if _, err := svc.Approve(ctx, requester, r.ID, ""); !errors.Is(err, access.ErrSelfApproval) {
		t.Errorf("self-approval = %v, want ErrSelfApproval", err)
	}
	if _, err := svc.Approve(ctx, approver1, r.ID, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := svc.Approve(ctx, approver1, r.ID, ""); !errors.Is(err, access.ErrAlreadyDecided) {
		t.Errorf("duplicate = %v, want ErrAlreadyDecided", err)
	}

	if _, err := svc.Reject(ctx, approver2, r.ID, "  "); !errors.Is(err, access.ErrReasonRequired) {
		t.Errorf("reasonless reject = %v, want ErrReasonRequired", err)
	}
	if view, err := svc.Reject(ctx, approver2, r.ID, "wrong table"); err != nil || view.Request.State != access.StateRejected {
		t.Errorf("reject = %+v, %v", view.Request, err)
	}
}

func TestCancelMatrix(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()

	d := createDraft(t, svc, connRead, "select 1")
	if got, err := svc.Cancel(ctx, requester, d.ID); err != nil || got.Request.State != access.StateCancelled {
		t.Errorf("draft cancel = %+v, %v", got, err)
	}
	p := createDraft(t, svc, connRead, "select 1")
	if _, err := svc.Submit(ctx, requester, p.ID, 1); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := svc.Cancel(ctx, approver1, p.ID); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("foreign cancel = %v, want ErrNotFound", err)
	}
	if got, err := svc.Cancel(ctx, requester, p.ID); err != nil || got.Request.State != access.StateCancelled {
		t.Errorf("pending cancel = %+v, %v", got, err)
	}
	if _, err := svc.Cancel(ctx, requester, p.ID); !errors.Is(err, access.ErrNotCancellable) {
		t.Errorf("re-cancel = %v, want ErrNotCancellable", err)
	}
	a := createDraft(t, svc, connAuto, "select 1")
	if _, err := svc.Submit(ctx, requester, a.ID, 1); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got, err := svc.Cancel(ctx, requester, a.ID); err != nil || got.Request.State != access.StateCancelled {
		t.Errorf("approved cancel = %+v, %v", got, err)
	}
}

func TestGetVisibility(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()
	r := createDraft(t, svc, connRead, "select secret from t")

	own, err := svc.Get(ctx, requester, false, r.ID)
	if err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if own.Payload == nil || own.Payload.SQL != "select secret from t" {
		t.Errorf("owner payload = %+v", own.Payload)
	}

	rev, err := svc.Get(ctx, approver1, true, r.ID)
	if err != nil || rev.Payload == nil {
		t.Errorf("reviewer Get = %+v, %v", rev.Payload, err)
	}

	if _, err := svc.Get(ctx, approver1, false, r.ID); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("foreign Get = %v, want ErrNotFound", err)
	}
}

func TestListScopeAndPaging(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()
	mine := createDraft(t, svc, connRead, "select 1")
	other, _ := access.NewDraft("req-other", "org-default", connRead, approver1, t0)
	repo.requests[other.ID] = other

	page, err := svc.List(ctx, requester, false, access.ListQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.TotalCount != 1 || page.Items[0].Request.ID != mine.ID {
		t.Errorf("own scope = %+v", page)
	}
	if page.Page != 1 || page.PageSize != 20 || page.TotalPages != 1 {
		t.Errorf("page defaults = %d/%d/%d", page.Page, page.PageSize, page.TotalPages)
	}

	if page, err = svc.List(ctx, approver1, true, access.ListQuery{}); err != nil || page.TotalCount != 2 {
		t.Errorf("reviewer scope = %+v, %v", page, err)
	}

	if page, err = svc.List(ctx, requester, false, access.ListQuery{PageSize: 37}); err != nil || page.PageSize != 20 {
		t.Errorf("off-list page size = %d, %v; want default 20", page.PageSize, err)
	}
	if page, err = svc.List(ctx, requester, false, access.ListQuery{PageSize: 50}); err != nil || page.PageSize != 50 {
		t.Errorf("listed page size = %d, %v; want 50", page.PageSize, err)
	}
	if _, err := svc.List(ctx, requester, false, access.ListQuery{State: access.State("bogus")}); !errors.Is(err, access.ErrInvalidRequest) {
		t.Errorf("bogus state = %v, want ErrInvalidRequest", err)
	}
}

func TestEffectiveStateComesFromTheQueryNotTheAppClock(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()

	approved := f0Approved(t, repo)
	repo.dbNow = func() time.Time { return approved.ExpiresAt.Add(time.Minute) }

	// …while the app's clock is still well before it (skew, or simply the time spent between the query and building the response).
	page, err := svc.List(ctx, requester, true, access.ListQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("page = %d items, want 1", len(page.Items))
	}
	if got := page.Items[0].EffectiveState; got != access.StateExpired {
		t.Errorf("badge = %s, want %s — the response recomputed what the query already decided", got, access.StateExpired)
	}
	if got := page.Items[0].EffectiveReason; got != access.ReasonTTLExpired {
		t.Errorf("badge reason = %q, want %q", got, access.ReasonTTLExpired)
	}

	view, err := svc.Get(ctx, requester, true, approved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := view.EffectiveState; got != access.StateExpired {
		t.Errorf("detail badge = %s, want %s", got, access.StateExpired)
	}

	// Adversarial: pushing the app clock far into the future must not change the answer either — neither machine's clock overrides the query's.
	svc.WithClock(func() time.Time { return approved.ExpiresAt.Add(72 * time.Hour) })
	repo.dbNow = func() time.Time { return approved.ExpiresAt.Add(-time.Minute) }
	page, err = svc.List(ctx, requester, true, access.ListQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := page.Items[0].EffectiveState; got != access.StateApproved {
		t.Errorf("badge = %s, want %s — the app clock overrode the query again", got, access.StateApproved)
	}
}

func f0Approved(t *testing.T, repo *fakeRepo) access.Request {
	t.Helper()
	r, err := access.NewDraft("req-approved", "org-default", connRead, requester, t0)
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := r.Submitted(access.Snapshot{
		Class: connection.ClassRead, PolicyVersion: 3, ConnectionConfigVersion: 1,
		ConnectionFingerprint: "v1|sha256:" + string(connRead),
		ConnectionDisplayName: "Connection " + string(connRead), ConnectionDBType: "postgresql",
		RequiredApprovals: 1,
		Digest:            []byte("digest"), DigestKeyVersion: 1, RedactedSQL: "select <integer>",
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := submitted.Approved(t0, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	repo.requests[approved.ID] = approved

	repo.sealed[approved.ID] = access.SealedPayload{
		KeyVersion: 1, WrappedDEK: []byte("dek"), Nonce: []byte("nonce"),
		Ciphertext: []byte(`{"SQL":"select 1","Params":[]}`),
	}
	return approved
}

func TestListClampsPageBeyondLast(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	ctx := context.Background()
	createDraft(t, svc, connRead, "select 1")

	repo.listCalls = 0
	page, err := svc.List(ctx, requester, false, access.ListQuery{Page: 5, PageSize: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Page != 1 || page.TotalPages != 1 || len(page.Items) != 1 {
		t.Errorf("page 5 of 1 = page %d/%d with %d items; want page 1/1 with 1 item", page.Page, page.TotalPages, len(page.Items))
	}
	// ONE read: the repository clamps inside its own snapshot, so the service no longer follows an empty page with a second call — two calls would be two snapshots, and the clamped page could be out of range again by then.
	if repo.listCalls != 1 {
		t.Errorf("repo List calls = %d, want 1 (the repository clamps within its snapshot)", repo.listCalls)
	}
}

func TestClassVocabulariesPinned(t *testing.T) {
	t.Parallel()
	pairs := map[query.StatementClass]connection.StatementClass{
		query.ClassRead:  connection.ClassRead,
		query.ClassWrite: connection.ClassWrite,
		query.ClassDDL:   connection.ClassDDL,
	}
	for q, c := range pairs {
		if string(q) != string(c) {
			t.Errorf("vocabulary drift: query %q vs connection %q", q, c)
		}
	}
}

func TestNewValidatesDeps(t *testing.T) {
	t.Parallel()
	if _, err := accessrequest.New(nil, nil, fakeCodec{}, fakeDialect{}, 24*time.Hour); err == nil {
		t.Error("nil repo must fail")
	}
	if _, err := accessrequest.New(newRepo(), newRepo(), fakeCodec{}, fakeDialect{}, time.Minute); err == nil {
		t.Error("validity below 15m must fail")
	}
	if _, err := accessrequest.New(newRepo(), newRepo(), fakeCodec{}, fakeDialect{}, 200*time.Hour); err == nil {
		t.Error("validity above 7d must fail")
	}
}

func TestCreateSurfacesArchivedFromTheWrite(t *testing.T) {
	t.Parallel()
	repo := newRepo()

	repo.policyReadIgnoresArchive = true
	svc := newService(t, repo)

	if _, err := svc.Create(context.Background(), requester, accessrequest.CreateParams{ConnectionID: connGone, SQL: "select 1"}); !errors.Is(err, access.ErrConnectionArchived) {
		t.Errorf("Create on archived = %v, want ErrConnectionArchived (from the write)", err)
	}
}

func TestSubmitSurfacesPolicyConflictFromTheWrite(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)
	r := createDraft(t, svc, connRead, "select 1")

	repo.bumpOnPolicyRead = true

	if _, err := svc.Submit(context.Background(), requester, r.ID, 1); !errors.Is(err, connection.ErrPolicyConflict) {
		t.Errorf("Submit across a policy change = %v, want connection.ErrPolicyConflict", err)
	}
	if got := repo.requests[r.ID]; got.State != access.StateDraft {
		t.Errorf("state after a refused submit = %q, want draft", got.State)
	}
}

func TestListRequestableConnections(t *testing.T) {
	t.Parallel()
	repo := newRepo()
	svc := newService(t, repo)

	conns, err := svc.ListRequestableConnections(context.Background())
	if err != nil {
		t.Fatalf("ListRequestableConnections: %v", err)
	}
	if len(conns) != 3 {
		t.Fatalf("requestable = %d connections, want 3 (the archived one excluded)", len(conns))
	}
	for _, c := range conns {
		if c.ID == connGone {
			t.Error("an archived connection must never be requestable (§4.3)")
		}
		if c.DisplayName == "" || c.DBType == "" || c.Environment == "" {
			t.Errorf("requestable connection missing a form field: %+v", c)
		}
	}
}
