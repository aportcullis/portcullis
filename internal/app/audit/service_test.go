package audit_test

import (
	"context"
	"errors"
	"testing"

	auditapp "github.com/aportcullis/portcullis/internal/app/audit"
	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type fakeReader struct {
	got     domainaudit.ListParams
	count   int64
	events  []domainaudit.Event
	event   domainaudit.Event
	getErr  error
	listErr error
	// clampedTo, when set, is the page the reader reports having actually read — the store clamps an out-of-range request inside its own snapshot, and the service must echo what came back rather than what was asked for.
	clampedTo int
	// organization is what DefaultOrganizationID resolves; listedOrg and fetchedOrg record what the service passed on.
	organization      identity.OrganizationID
	emptyOrganization bool
	orgErr            error
	listedOrg         identity.OrganizationID
	fetchedOrg        identity.OrganizationID
}

func (f *fakeReader) DefaultOrganizationID(context.Context) (identity.OrganizationID, error) {
	if f.organization == "" && !f.emptyOrganization {
		return "org-default", f.orgErr
	}
	return f.organization, f.orgErr
}

func (f *fakeReader) List(_ context.Context, org identity.OrganizationID, p domainaudit.ListParams) (domainaudit.EventPage, error) {
	f.got = p
	f.listedOrg = org
	read := p.Page
	if f.clampedTo != 0 {
		read = f.clampedTo
	}
	return domainaudit.EventPage{Events: f.events, Page: read, TotalCount: f.count}, f.listErr
}

func (f *fakeReader) Get(_ context.Context, org identity.OrganizationID, _ string) (domainaudit.Event, error) {
	f.fetchedOrg = org
	return f.event, f.getErr
}

func TestReadsAreScopedToTheResolvedOrganization(t *testing.T) {
	ctx := context.Background()
	for _, org := range []identity.OrganizationID{"org-default", "org-b", "6f1f2c1e-0000-4000-8000-000000000001"} {
		f := &fakeReader{organization: org}
		svc := newService(t, f)
		if _, err := svc.List(ctx, auditapp.Query{Page: 1, PageSize: 20}); err != nil {
			t.Fatalf("List: %v", err)
		}
		if _, err := svc.Get(ctx, "event-1"); err != nil {
			t.Fatalf("Get: %v", err)
		}
		if f.listedOrg != org || f.fetchedOrg != org {
			t.Errorf("reader saw list org %q / get org %q, want %q for both", f.listedOrg, f.fetchedOrg, org)
		}
	}

	sentinel := errors.New("organization lookup failed")
	for _, f := range []*fakeReader{{orgErr: sentinel}, {orgErr: sentinel, organization: "org-b"}} {
		svc := newService(t, f)
		if _, err := svc.List(ctx, auditapp.Query{Page: 1, PageSize: 20}); !errors.Is(err, sentinel) {
			t.Errorf("List with failed org lookup = %v, want %v", err, sentinel)
		}
		if _, err := svc.Get(ctx, "event-1"); !errors.Is(err, sentinel) {
			t.Errorf("Get with failed org lookup = %v, want %v", err, sentinel)
		}
		if f.listedOrg != "" || f.fetchedOrg != "" {
			t.Errorf("reader was called without an organization: list %q / get %q", f.listedOrg, f.fetchedOrg)
		}
	}

	empty := &fakeReader{emptyOrganization: true}
	svc := newService(t, empty)
	if _, err := svc.List(ctx, auditapp.Query{Page: 1, PageSize: 20}); err == nil {
		t.Error("List with an empty resolved organization succeeded, want refusal")
	}
	if _, err := svc.Get(ctx, "event-1"); err == nil {
		t.Error("Get with an empty resolved organization succeeded, want refusal")
	}
}

func newService(t *testing.T, r *fakeReader) *auditapp.Service {
	t.Helper()
	svc, err := auditapp.New(r)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}

func TestListNormalizesPageSizeToWhitelist(t *testing.T) {
	for _, c := range []struct {
		in, want int
	}{
		{0, 20}, {20, 20}, {15, 20}, {10, 10}, {50, 50}, {100, 100}, {200, 20}, {-3, 20},
	} {
		f := &fakeReader{}
		page, err := newService(t, f).List(context.Background(), auditapp.Query{Page: 1, PageSize: c.in})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if page.PageSize != c.want || f.got.PageSize != c.want {
			t.Errorf("page_size %d → echoed %d / asked the reader for %d, want %d",
				c.in, page.PageSize, f.got.PageSize, c.want)
		}
	}
}

func TestListFloorsPageAndEchoesThePageThatWasRead(t *testing.T) {
	f := &fakeReader{}
	page, err := newService(t, f).List(context.Background(), auditapp.Query{Page: 3, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Page != 3 || f.got.Page != 3 {
		t.Errorf("page=%d asked for %d, want 3 and 3", page.Page, f.got.Page)
	}

	floored := &fakeReader{}
	belowOne, _ := newService(t, floored).List(context.Background(), auditapp.Query{Page: -5, PageSize: 20})
	if belowOne.Page != 1 || floored.got.Page != 1 {
		t.Errorf("page<1 → echoed %d, asked for %d; want 1 and 1", belowOne.Page, floored.got.Page)
	}

	clamped := &fakeReader{clampedTo: 2, count: 25}
	overflow, err := newService(t, clamped).List(context.Background(), auditapp.Query{Page: 9, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if overflow.Page != 2 || overflow.TotalPages != 2 {
		t.Errorf("clamped page = %d of %d, want 2 of 2", overflow.Page, overflow.TotalPages)
	}
}

func TestListComputesTotalPages(t *testing.T) {
	for _, c := range []struct {
		total    int64
		pageSize int
		want     int
	}{
		{0, 20, 0}, {1, 20, 1}, {20, 20, 1}, {21, 20, 2}, {1340, 20, 67}, {100, 50, 2},
	} {
		f := &fakeReader{count: c.total}
		page, err := newService(t, f).List(context.Background(), auditapp.Query{Page: 1, PageSize: c.pageSize})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if page.TotalCount != c.total || page.TotalPages != c.want {
			t.Errorf("total=%d size=%d → total_pages=%d, want %d", c.total, c.pageSize, page.TotalPages, c.want)
		}
	}
}

func TestListSortDirectionDefaultsToNewestFirst(t *testing.T) {
	f := &fakeReader{}
	if _, err := newService(t, f).List(context.Background(), auditapp.Query{Page: 1, PageSize: 20}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !f.got.SortDescending {
		t.Error("default sort must be descending (newest first)")
	}

	f2 := &fakeReader{}
	if _, err := newService(t, f2).List(context.Background(), auditapp.Query{Page: 1, PageSize: 20, SortAscending: true}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if f2.got.SortDescending {
		t.Error("SortAscending must produce an ascending store query")
	}
}

func TestListAcceptsWhitelistedSortField(t *testing.T) {
	f := &fakeReader{}
	if _, err := newService(t, f).List(context.Background(), auditapp.Query{Page: 1, PageSize: 20, SortField: "occurred_at"}); err != nil {
		t.Fatalf("List with occurred_at: %v", err)
	}
}

func TestListRejectsOffWhitelistSortField(t *testing.T) {
	f := &fakeReader{}
	_, err := newService(t, f).List(context.Background(), auditapp.Query{Page: 1, PageSize: 20, SortField: "actor_user_id"})
	if !errors.Is(err, auditapp.ErrInvalidSortField) {
		t.Fatalf("expected ErrInvalidSortField, got %v", err)
	}
}

func TestListPropagatesReaderErrors(t *testing.T) {
	sentinel := errors.New("db down")
	if _, err := newService(t, &fakeReader{listErr: sentinel}).List(context.Background(), auditapp.Query{Page: 1, PageSize: 20}); !errors.Is(err, sentinel) {
		t.Fatalf("List error not propagated: %v", err)
	}
}

func TestGetPreservesReaderResult(t *testing.T) {
	want := domainaudit.Event{ID: "event-1", Action: domainaudit.ActionAuthLogin}
	got, err := newService(t, &fakeReader{event: want}).Get(context.Background(), want.ID)
	if err != nil || got.ID != want.ID || got.Action != want.Action {
		t.Fatalf("Get = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	sentinel := errors.New("db down")
	if _, err := newService(t, &fakeReader{getErr: sentinel}).Get(context.Background(), "event-1"); !errors.Is(err, sentinel) {
		t.Fatalf("Get error not propagated: %v", err)
	}
}

func TestNewRejectsNilReader(t *testing.T) {
	if _, err := auditapp.New(nil); err == nil {
		t.Fatal("expected error for nil reader")
	}
}
