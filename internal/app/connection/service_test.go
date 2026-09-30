package connection_test

import (
	"context"
	"errors"
	"testing"
	"time"

	appconn "github.com/aportcullis/portcullis/internal/app/connection"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// fakeRepo is an in-memory Repository. Mutations append the events they were handed to txEvents, mimicking the real store's same-transaction write (ADR-0009), so tests can assert which events ride which mutation.
type fakeRepo struct {
	conns    map[connection.ConnectionID]*connection.Connection
	sealed   map[connection.ConnectionID]connection.SealedCredential
	txEvents [][]audit.Event
	org      identity.OrganizationID

	createCalls  int
	replaceCalls int
	renameCalls  int
	// descriptorExpectedVersion records the optimistic token the descriptor update was guarded with — it must be the version of the row the service read, never a number the client chose.
	descriptorExpectedVersion int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		conns:  map[connection.ConnectionID]*connection.Connection{},
		sealed: map[connection.ConnectionID]connection.SealedCredential{},
		org:    "org1",
	}
}

func (r *fakeRepo) DefaultOrganizationID(_ context.Context) (identity.OrganizationID, error) {
	return r.org, nil
}

func (r *fakeRepo) Create(_ context.Context, c connection.Connection, cred connection.SealedCredential, events ...audit.Event) error {
	r.createCalls++
	for id, existing := range r.conns {
		if id != c.ID && !existing.IsArchived() && existing.DisplayName == c.DisplayName {
			return connection.ErrNameTaken
		}
	}
	r.conns[c.ID] = &c
	r.sealed[c.ID] = cred
	r.txEvents = append(r.txEvents, events)
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, error) {
	c, ok := r.conns[id]
	if !ok || org != r.org {
		return connection.Connection{}, connection.ErrNotFound
	}
	return *c, nil
}

func (r *fakeRepo) List(_ context.Context, org identity.OrganizationID, includeArchived bool) ([]connection.Connection, error) {
	var out []connection.Connection
	for _, c := range r.conns {
		if org == r.org && (includeArchived || !c.IsArchived()) {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (r *fakeRepo) UpdateDescriptor(_ context.Context, org identity.OrganizationID, id connection.ConnectionID, displayName string, env connection.Environment, description string, expectedVersion int64, events ...audit.Event) (connection.Connection, error) {
	r.renameCalls++
	r.descriptorExpectedVersion = expectedVersion
	c, ok := r.conns[id]
	if !ok || org != r.org {
		return connection.Connection{}, connection.ErrNotFound
	}

	if c.Version != expectedVersion {
		return connection.Connection{}, connection.ErrConflict
	}
	c.DisplayName = displayName
	c.Environment = env
	c.Description = description
	c.Version++
	r.txEvents = append(r.txEvents, events)
	return *c, nil
}

func (r *fakeRepo) ReplaceConfig(_ context.Context, c connection.Connection, expectedVersion int64, cred connection.SealedCredential, events ...audit.Event) (connection.Connection, error) {
	r.replaceCalls++
	existing, ok := r.conns[c.ID]
	if !ok {
		return connection.Connection{}, connection.ErrNotFound
	}
	if existing.IsArchived() {
		return connection.Connection{}, connection.ErrArchived
	}
	if existing.Version != expectedVersion {
		return connection.Connection{}, connection.ErrConflict
	}
	c.Version = existing.Version + 1
	r.conns[c.ID] = &c
	r.sealed[c.ID] = cred
	r.txEvents = append(r.txEvents, events)
	return c, nil
}

func (r *fakeRepo) Archive(_ context.Context, org identity.OrganizationID, id connection.ConnectionID, events ...audit.Event) (connection.Connection, error) {
	c, ok := r.conns[id]
	if !ok || org != r.org {
		return connection.Connection{}, connection.ErrNotFound
	}
	if c.IsArchived() {
		return connection.Connection{}, connection.ErrAlreadyArchived
	}
	now := time.Now()
	c.ArchivedAt = &now
	c.Version++
	delete(r.sealed, id)
	r.txEvents = append(r.txEvents, events)
	return *c, nil
}

func (r *fakeRepo) TestMaterial(_ context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Connection, connection.SealedCredential, error) {
	c, ok := r.conns[id]
	if !ok || org != r.org {
		return connection.Connection{}, connection.SealedCredential{}, connection.ErrNotFound
	}
	if c.IsArchived() {
		return connection.Connection{}, connection.SealedCredential{}, connection.ErrArchived
	}
	return *c, r.sealed[id], nil
}

type fakeValidator struct {
	err           error
	calls         int
	afterValidate func()

	lastTarget connection.Target
	lastMode   connection.TLSMode
	lastCred   connection.Credential
}

func (t *fakeValidator) ValidateConnection(_ context.Context, target connection.Target, mode connection.TLSMode, cred connection.Credential) error {
	t.calls++
	t.lastTarget, t.lastMode, t.lastCred = target, mode, cred
	if t.afterValidate != nil {
		t.afterValidate()
	}
	return t.err
}

type fakeCodec struct {
	sealedOrg identity.OrganizationID
	sealedID  connection.ConnectionID
	plain     map[connection.ConnectionID]connection.Credential
	sealErr   error
}

func newFakeCodec() *fakeCodec {
	return &fakeCodec{plain: map[connection.ConnectionID]connection.Credential{}}
}

func (c *fakeCodec) Seal(org identity.OrganizationID, id connection.ConnectionID, cred connection.Credential) (connection.SealedCredential, error) {
	if c.sealErr != nil {
		return connection.SealedCredential{}, c.sealErr
	}
	c.sealedOrg, c.sealedID = org, id
	c.plain[id] = cred
	return connection.SealedCredential{KeyVersion: 1, Ciphertext: []byte(cred.User)}, nil
}

func (c *fakeCodec) Open(org identity.OrganizationID, id connection.ConnectionID, _ connection.SealedCredential) (connection.Credential, error) {
	cred, ok := c.plain[id]
	if !ok || org != c.sealedOrg && c.sealedOrg != "" {
		return connection.Credential{}, errors.New("fake codec: unknown sealed credential")
	}
	return cred, nil
}

type fakeAuditor struct{ events []audit.Event }

func (a *fakeAuditor) Record(_ context.Context, e audit.Event) error {
	a.events = append(a.events, e)
	return nil
}

type fixture struct {
	svc       *appconn.Service
	repo      *fakeRepo
	validator *fakeValidator
	codec     *fakeCodec
	auditor   *fakeAuditor
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	repo := newFakeRepo()
	validator := &fakeValidator{}
	codec := newFakeCodec()
	auditor := &fakeAuditor{}
	svc, err := appconn.New(repo, validator, codec, auditor)
	if err != nil {
		t.Fatal(err)
	}
	ids := 0
	svc.WithIDGenerator(func() string { ids++; return "conn-" + string(rune('0'+ids)) }).
		WithClock(func() time.Time { return time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC) })
	return fixture{svc: svc, repo: repo, validator: validator, codec: codec, auditor: auditor}
}

func description(text string) *string { return &text }

func validCreate() appconn.CreateParams {
	return appconn.CreateParams{
		DisplayName: "Prod replica",
		Config: appconn.ConfigInput{
			Host: "db.example.com", Port: 5432, Database: "appdb",
			User: "reader", Password: "s3cret", TLSMode: "",
		},
	}
}

func TestNewRejectsNilDependencies(t *testing.T) {
	t.Parallel()
	if _, err := appconn.New(nil, &fakeValidator{}, newFakeCodec(), &fakeAuditor{}); err == nil {
		t.Error("New with nil repo should fail")
	}
	if _, err := appconn.New(newFakeRepo(), nil, newFakeCodec(), &fakeAuditor{}); err == nil {
		t.Error("New with nil validator should fail")
	}
	if _, err := appconn.New(newFakeRepo(), &fakeValidator{}, nil, &fakeAuditor{}); err == nil {
		t.Error("New with nil codec should fail")
	}
	if _, err := appconn.New(newFakeRepo(), &fakeValidator{}, newFakeCodec(), nil); err == nil {
		t.Error("New with nil auditor should fail")
	}
}

func TestCreateWithFailingTestPersistsNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.validator.err = &connection.TestError{Bucket: connection.TestBucketAuthFailed}

	_, err := f.svc.Create(t.Context(), "admin1", validCreate())

	var te *connection.TestError
	if !errors.As(err, &te) || te.Bucket != connection.TestBucketAuthFailed {
		t.Fatalf("Create err = %v, want TestError{auth-failed}", err)
	}
	if f.repo.createCalls != 0 {
		t.Error("repo.Create must not be called when the test fails")
	}
	if len(f.auditor.events) != 1 || f.auditor.events[0].Action != audit.ActionConnectionTest ||
		f.auditor.events[0].Outcome != audit.OutcomeFailed {
		t.Fatalf("want one best-effort CONNECTION_TEST FAILED event, got %+v", f.auditor.events)
	}
	if got := f.auditor.events[0].Metadata["reason"]; got != string(connection.TestBucketAuthFailed) {
		t.Errorf("test event reason = %v, want auth-failed bucket", got)
	}
}

func TestCreateDefaultTLSRecordsExactlyCreated(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	got, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	if got.TLSMode != connection.TLSModeVerifyFull {
		t.Errorf("TLSMode = %q, want default verify-full", got.TLSMode)
	}
	if len(f.repo.txEvents) != 1 {
		t.Fatalf("want 1 transactional mutation, got %d", len(f.repo.txEvents))
	}
	events := f.repo.txEvents[0]
	if len(events) != 1 || events[0].Action != audit.ActionConnectionCreated {
		t.Fatalf("want exactly [CONNECTION_CREATED], got %+v", events)
	}
	if events[0].TargetType != audit.TargetTypeConnection || events[0].TargetID != string(got.ID) {
		t.Errorf("event target = %s/%s, want connection/%s", events[0].TargetType, events[0].TargetID, got.ID)
	}
	if events[0].ActorUserID == nil || *events[0].ActorUserID != "admin1" {
		t.Errorf("event actor = %v, want admin1", events[0].ActorUserID)
	}
	if events[0].OrganizationID != "org1" {
		t.Errorf("event org = %q, want org1 (resolved before the tx)", events[0].OrganizationID)
	}
	if len(f.auditor.events) != 0 {
		t.Errorf("no best-effort events expected on a successful create, got %+v", f.auditor.events)
	}
}

func TestCreateRelaxedTLSAddsRelaxedEventInSameTx(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	p := validCreate()
	p.Config.TLSMode = "require"

	got, err := f.svc.Create(t.Context(), "admin1", p)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.repo.txEvents) != 1 {
		t.Fatalf("want 1 mutation, got %d", len(f.repo.txEvents))
	}
	events := f.repo.txEvents[0]
	if len(events) != 2 ||
		events[0].Action != audit.ActionConnectionCreated ||
		events[1].Action != audit.ActionConnectionTLSRelaxed {
		t.Fatalf("want [CONNECTION_CREATED, CONNECTION_TLS_RELAXED] in one tx, got %+v", events)
	}
	if got := events[1].Metadata["tls_mode"]; got != "require" {
		t.Errorf("relaxed event tls_mode = %v, want require", got)
	}
	if got.TLSMode != connection.TLSModeRequire {
		t.Errorf("TLSMode = %q, want require", got.TLSMode)
	}
}

func TestCreateSealsUnderGeneratedIDAndOrg(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	got, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	if f.codec.sealedID != got.ID || f.codec.sealedOrg != "org1" {
		t.Errorf("sealed under (%s, %s), want (%s, org1)", f.codec.sealedOrg, f.codec.sealedID, got.ID)
	}
	if f.validator.calls != 1 || f.validator.lastCred.User != "reader" || f.validator.lastCred.Password != "s3cret" {
		t.Errorf("tester saw %+v, want the submitted credential", f.validator.lastCred)
	}
}

func TestCreateValidationFailuresSkipTester(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*appconn.CreateParams)
		wantErr error
	}{
		{"bad tls mode", func(p *appconn.CreateParams) { p.Config.TLSMode = "prefer" }, connection.ErrInvalidTLSMode},
		{"bad target", func(p *appconn.CreateParams) { p.Config.Port = 0 }, connection.ErrInvalidTarget},
		{"bad credential", func(p *appconn.CreateParams) { p.Config.User = "" }, connection.ErrInvalidCredential},
		{"bad display name", func(p *appconn.CreateParams) { p.DisplayName = " " }, connection.ErrInvalidDisplayName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			p := validCreate()
			tt.mutate(&p)
			if _, err := f.svc.Create(t.Context(), "admin1", p); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if f.validator.calls != 0 {
				t.Error("tester must not be dialed for invalid input")
			}
			if f.repo.createCalls != 0 {
				t.Error("nothing may persist for invalid input")
			}
		})
	}
}

func TestUpdateDescriptorOnlyEmptyNameKeepsCurrent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	p := validCreate()
	p.DisplayName = "Keep me"
	created, err := f.svc.Create(t.Context(), "admin1", p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
		Environment: "production", ExpectedVersion: created.Version,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.DisplayName != "Keep me" {
		t.Errorf("DisplayName = %q, want the current name kept", got.DisplayName)
	}
	if got.Environment != connection.EnvironmentProduction {
		t.Errorf("Environment = %q, want production applied", got.Environment)
	}
}

func TestUpdateRequiresTheVersionTheEditorRead(t *testing.T) {
	t.Parallel()

	t.Run("a stale token is refused before the store is touched", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		created, err := f.svc.Create(t.Context(), "admin1", validCreate())
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
			Description: description("theirs"), ExpectedVersion: created.Version,
		}); err != nil {
			t.Fatalf("first update: %v", err)
		}
		calls := f.repo.renameCalls

		if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
			DisplayName: "Renamed", Description: description(""), ExpectedVersion: created.Version,
		}); !errors.Is(err, connection.ErrConflict) {
			t.Errorf("stale edit = %v, want ErrConflict", err)
		}
		if f.repo.renameCalls != calls {
			t.Errorf("the store was asked %d extra times; a version the service knows is stale never reaches it",
				f.repo.renameCalls-calls)
		}
		got, err := f.svc.Get(t.Context(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Description != "theirs" || got.DisplayName != created.DisplayName {
			t.Errorf("row = %q/%q, want the first update intact", got.DisplayName, got.Description)
		}
	})

	t.Run("guards with the version it read, not the number handed in", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		created, err := f.svc.Create(t.Context(), "admin1", validCreate())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
			DisplayName: "Renamed", ExpectedVersion: created.Version,
		}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if f.repo.descriptorExpectedVersion != created.Version {
			t.Errorf("store guarded with %d, want the read row's %d", f.repo.descriptorExpectedVersion, created.Version)
		}
	})

	t.Run("adversarial: a future token cannot pre-claim the next version", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		created, err := f.svc.Create(t.Context(), "admin1", validCreate())
		if err != nil {
			t.Fatal(err)
		}
		for _, version := range []int64{created.Version + 1, created.Version + 99, created.Version - 1, 0, -5} {
			if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
				DisplayName: "Hijacked", ExpectedVersion: version,
			}); !errors.Is(err, connection.ErrConflict) {
				t.Errorf("edit with version %d = %v, want ErrConflict", version, err)
			}
		}
		got, err := f.svc.Get(t.Context(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DisplayName == "Hijacked" {
			t.Error("a refused edit landed anyway")
		}
	})

	t.Run("the config flow is guarded the same way", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		created, err := f.svc.Create(t.Context(), "admin1", validCreate())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
			Description: description("theirs"), ExpectedVersion: created.Version,
		}); err != nil {
			t.Fatalf("first update: %v", err)
		}
		cfg := validCreate().Config
		if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
			DisplayName: "Renamed", Config: &cfg, ExpectedVersion: created.Version,
		}); !errors.Is(err, connection.ErrConflict) {
			t.Errorf("stale config replace = %v, want ErrConflict", err)
		}
	})
}

func TestUpdateRenameOnlySkipsTest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	f.validator.calls = 0

	got, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
		DisplayName: "Renamed", ExpectedVersion: created.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Renamed" {
		t.Errorf("DisplayName = %q, want Renamed", got.DisplayName)
	}
	if f.validator.calls != 0 {
		t.Error("rename-only must not dial the target")
	}
	if f.repo.renameCalls != 1 || f.repo.replaceCalls != 0 {
		t.Errorf("want Rename (not ReplaceConfig), got rename=%d replace=%d", f.repo.renameCalls, f.repo.replaceCalls)
	}
	last := f.repo.txEvents[len(f.repo.txEvents)-1]
	if len(last) != 1 || last[0].Action != audit.ActionConnectionUpdated {
		t.Fatalf("want [CONNECTION_UPDATED], got %+v", last)
	}
}

func TestUpdateConfigRetestsAndReseals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	f.validator.calls = 0

	cfg := validCreate().Config
	cfg.Host = "replica.example.com"
	cfg.TLSMode = "disable"
	got, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
		Config: &cfg, ExpectedVersion: created.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.validator.calls != 1 {
		t.Error("a config change must re-run the connection test")
	}
	if got.Target.Host != "replica.example.com" || got.TLSMode != connection.TLSModeDisable {
		t.Errorf("updated conn = %+v, want new host and tls mode", got)
	}
	if got.Fingerprint == created.Fingerprint {
		t.Error("fingerprint must be re-derived for the new target")
	}
	if got.Version != created.Version+1 {
		t.Errorf("config update version = %d, want %d even with the fixed test clock", got.Version, created.Version+1)
	}
	last := f.repo.txEvents[len(f.repo.txEvents)-1]
	if len(last) != 2 || last[0].Action != audit.ActionConnectionUpdated || last[1].Action != audit.ActionConnectionTLSRelaxed {
		t.Fatalf("want [CONNECTION_UPDATED, CONNECTION_TLS_RELAXED], got %+v", last)
	}
	// A failing test must abort the update. The row moved on with the successful update above, so this one carries the version it left behind.
	f.validator.err = &connection.TestError{Bucket: connection.TestBucketUnreachable}
	if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
		Config: &cfg, ExpectedVersion: got.Version,
	}); err == nil {
		t.Fatal("config update with a failing test must not persist")
	}
}

func TestUpdateConfigAuditsActualFieldScope(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	lastFields := func() []string {
		t.Helper()
		events := f.repo.txEvents[len(f.repo.txEvents)-1]
		fields, ok := events[0].Metadata["fields"].([]string)
		if !ok {
			t.Fatalf("CONNECTION_UPDATED metadata has no fields slice: %+v", events[0].Metadata)
		}
		return fields
	}

	cfg := validCreate().Config
	tests := []struct {
		name        string
		displayName string
		want        []string
	}{
		{"config only", "", []string{"config"}},
		{"same name resent", created.DisplayName, []string{"config"}},
		{"actual rename", "Renamed with config", []string{"config", "display_name"}},
	}
	for _, tt := range tests {

		current, err := f.svc.Get(t.Context(), created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
			DisplayName: tt.displayName, Config: &cfg, ExpectedVersion: current.Version,
		}); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		got := lastFields()
		if len(got) != len(tt.want) {
			t.Errorf("%s: fields = %v, want %v", tt.name, got, tt.want)
			continue
		}
		for idx := range tt.want {
			if got[idx] != tt.want[idx] {
				t.Errorf("%s: fields = %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

func TestUpdateConfigRejectsConcurrentRename(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	f.validator.afterValidate = func() {
		current := f.repo.conns[created.ID]
		current.DisplayName = "Renamed while testing"
		current.Version++
	}

	cfg := validCreate().Config
	cfg.Host = "replica.example.com"
	// The token is CURRENT when the update starts; the rename lands during the external test, so only the store's condition can catch it — this is the window the app-level guard cannot see.
	if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
		Config: &cfg, ExpectedVersion: created.Version,
	}); !errors.Is(err, connection.ErrConflict) {
		t.Fatalf("Update config after concurrent rename = %v, want ErrConflict", err)
	}
	if got := f.repo.conns[created.ID].DisplayName; got != "Renamed while testing" {
		t.Errorf("concurrent rename was overwritten: got %q", got)
	}
}

func TestArchiveDiscardsCredentialAndRefusesTwice(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.Archive(t.Context(), "admin1", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsArchived() {
		t.Error("returned connection should be archived")
	}
	last := f.repo.txEvents[len(f.repo.txEvents)-1]
	if len(last) != 1 || last[0].Action != audit.ActionConnectionArchived {
		t.Fatalf("want [CONNECTION_ARCHIVED], got %+v", last)
	}
	if _, err := f.svc.Archive(t.Context(), "admin1", created.ID); !errors.Is(err, connection.ErrAlreadyArchived) {
		t.Fatalf("second archive err = %v, want ErrAlreadyArchived", err)
	}
	// The credential is gone: test-by-id must refuse rather than dial.
	f.validator.calls = 0
	if err := f.svc.TestByID(t.Context(), "admin1", created.ID); !errors.Is(err, connection.ErrArchived) {
		t.Fatalf("TestByID on archived err = %v, want ErrArchived", err)
	}
	if f.validator.calls != 0 {
		t.Error("archived connection must not be dialed")
	}
}

func TestTestByIDOpensSealedCredential(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	f.validator.calls = 0

	if err := f.svc.TestByID(t.Context(), "admin1", created.ID); err != nil {
		t.Fatal(err)
	}
	if f.validator.calls != 1 || f.validator.lastCred.User != "reader" {
		t.Errorf("tester dialed with %+v, want the opened credential", f.validator.lastCred)
	}
	if len(f.auditor.events) != 1 || f.auditor.events[0].Action != audit.ActionConnectionTest ||
		f.auditor.events[0].Outcome != audit.OutcomeSucceeded ||
		f.auditor.events[0].TargetID != string(created.ID) {
		t.Fatalf("want CONNECTION_TEST SUCCEEDED for %s, got %+v", created.ID, f.auditor.events)
	}
}

func TestUpdateSealsUnderStoredID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	const alias = connection.ConnectionID("CONN-ALIAS")
	f.repo.conns[alias] = f.repo.conns[created.ID]
	f.repo.sealed[alias] = f.repo.sealed[created.ID]

	cfg := validCreate().Config
	cfg.Host = "replica.example.com"
	if _, err := f.svc.Update(t.Context(), "admin1", alias, appconn.UpdateParams{
		Config: &cfg, ExpectedVersion: created.Version,
	}); err != nil {
		t.Fatal(err)
	}
	if f.codec.sealedID != created.ID {
		t.Errorf("resealed under %q, want the stored id %q", f.codec.sealedID, created.ID)
	}
}

func TestTestByIDOpensUnderStoredID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	const alias = connection.ConnectionID("CONN-ALIAS")
	f.repo.conns[alias] = f.repo.conns[created.ID]
	f.repo.sealed[alias] = f.repo.sealed[created.ID]
	f.validator.calls = 0

	if err := f.svc.TestByID(t.Context(), "admin1", alias); err != nil {
		t.Fatalf("TestByID through an alias spelling must open under the stored id: %v", err)
	}
	if f.validator.calls != 1 {
		t.Error("the stored credential should have been opened and dialed")
	}
	last := f.auditor.events[len(f.auditor.events)-1]
	if last.TargetID != string(created.ID) {
		t.Errorf("CONNECTION_TEST target = %q, want the stored id %q", last.TargetID, created.ID)
	}
}

func TestArchiveDuringDialLetsAdmittedTestComplete(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	f.validator.afterValidate = func() {

		now := time.Now()
		f.repo.conns[created.ID].ArchivedAt = &now
		delete(f.repo.sealed, created.ID)
	}
	f.auditor.events = nil

	if err := f.svc.TestByID(t.Context(), "admin1", created.ID); err != nil {
		t.Fatalf("admitted test must complete despite the concurrent archive: %v", err)
	}
	if len(f.auditor.events) != 1 || f.auditor.events[0].Action != audit.ActionConnectionTest ||
		f.auditor.events[0].Outcome != audit.OutcomeSucceeded {
		t.Fatalf("want the completed test's CONNECTION_TEST SUCCEEDED, got %+v", f.auditor.events)
	}

	f.validator.afterValidate = nil
	f.validator.calls = 0
	if err := f.svc.TestByID(t.Context(), "admin1", created.ID); !errors.Is(err, connection.ErrArchived) {
		t.Fatalf("new test after archive = %v, want ErrArchived", err)
	}
	if f.validator.calls != 0 {
		t.Error("an archived connection must not be dialed")
	}
}

func TestTestByConfig(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.validator.err = &connection.TestError{Bucket: connection.TestBucketTLSFailed}

	err := f.svc.TestByConfig(t.Context(), "admin1", validCreate().Config)
	var te *connection.TestError
	if !errors.As(err, &te) || te.Bucket != connection.TestBucketTLSFailed {
		t.Fatalf("err = %v, want TestError{tls-failed}", err)
	}
	if f.repo.createCalls != 0 {
		t.Error("TestByConfig must not persist anything")
	}
	if len(f.auditor.events) != 1 || f.auditor.events[0].Outcome != audit.OutcomeFailed {
		t.Fatalf("want one best-effort FAILED event, got %+v", f.auditor.events)
	}
}

func TestConnectionTestEventsCarryTLSMode(t *testing.T) {
	t.Parallel()

	t.Run("explicit test success", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		cfg := validCreate().Config
		cfg.TLSMode = "disable"
		if err := f.svc.TestByConfig(t.Context(), "admin1", cfg); err != nil {
			t.Fatal(err)
		}
		if got := f.auditor.events[0].Metadata["tls_mode"]; got != "disable" {
			t.Errorf("tls_mode = %v, want disable", got)
		}
	})

	t.Run("explicit test failure", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.validator.err = &connection.TestError{Bucket: connection.TestBucketUnreachable}
		if err := f.svc.TestByConfig(t.Context(), "admin1", validCreate().Config); err == nil {
			t.Fatal("want the test failure surfaced")
		}
		if got := f.auditor.events[0].Metadata["tls_mode"]; got != "verify-full" {
			t.Errorf("tls_mode = %v, want the verify-full default", got)
		}
	})

	t.Run("unsaved dial", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.codec.sealErr = errors.New("boom")
		p := validCreate()
		p.Config.TLSMode = "require"
		if _, err := f.svc.Create(t.Context(), "admin1", p); err == nil {
			t.Fatal("want the seal failure surfaced")
		}
		e := f.auditor.events[0]
		if got, ok := e.Metadata["persisted"].(bool); !ok || got {
			t.Fatalf("want the persisted=false unsaved-dial event, got %+v", e)
		}
		if got := e.Metadata["tls_mode"]; got != "require" {
			t.Errorf("tls_mode = %v, want require", got)
		}
	})
}

func TestGetAndList(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	created, err := f.svc.Create(t.Context(), "admin1", validCreate())
	if err != nil {
		t.Fatal(err)
	}
	p2 := validCreate()
	p2.DisplayName = "Second"
	second, err := f.svc.Create(t.Context(), "admin1", p2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Archive(t.Context(), "admin1", second.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.Get(t.Context(), created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	active, err := f.svc.List(t.Context(), false)
	if err != nil || len(active) != 1 {
		t.Fatalf("List(active) = %d conns, %v; want 1", len(active), err)
	}
	all, err := f.svc.List(t.Context(), true)
	if err != nil || len(all) != 2 {
		t.Fatalf("List(all) = %d conns, %v; want 2", len(all), err)
	}
	if _, err := f.svc.Get(t.Context(), "missing"); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestCreateFailureAfterSuccessfulDialLeavesTestTrail(t *testing.T) {
	t.Parallel()

	t.Run("repository conflict", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		if _, err := f.svc.Create(t.Context(), "admin1", validCreate()); err != nil {
			t.Fatal(err)
		}
		f.auditor.events = nil

		_, err := f.svc.Create(t.Context(), "admin1", validCreate())
		if !errors.Is(err, connection.ErrNameTaken) {
			t.Fatalf("err = %v, want ErrNameTaken", err)
		}
		assertUnsavedDialEvent(t, f.auditor.events)
	})

	t.Run("seal failure", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.codec.sealErr = errors.New("keyring exploded")

		if _, err := f.svc.Create(t.Context(), "admin1", validCreate()); err == nil {
			t.Fatal("Create with failing sealer should error")
		}
		assertUnsavedDialEvent(t, f.auditor.events)
	})

	t.Run("update replace failure", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		created, err := f.svc.Create(t.Context(), "admin1", validCreate())
		if err != nil {
			t.Fatal(err)
		}
		f.auditor.events = nil
		f.codec.sealErr = errors.New("keyring exploded")

		cfg := validCreate().Config
		if _, err := f.svc.Update(t.Context(), "admin1", created.ID, appconn.UpdateParams{
			Config: &cfg, ExpectedVersion: created.Version,
		}); err == nil {
			t.Fatal("Update with failing sealer should error")
		}
		assertUnsavedDialEvent(t, f.auditor.events)
	})
}

func assertUnsavedDialEvent(t *testing.T, events []audit.Event) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("want exactly one best-effort event, got %+v", events)
	}
	e := events[0]
	if e.Action != audit.ActionConnectionTest || e.Outcome != audit.OutcomeSucceeded {
		t.Fatalf("event = %s/%s, want CONNECTION_TEST SUCCEEDED", e.Action, e.Outcome)
	}
	if got, ok := e.Metadata["persisted"].(bool); !ok || got {
		t.Errorf("metadata persisted = %v, want false — the dial happened but nothing was saved", e.Metadata["persisted"])
	}
}
