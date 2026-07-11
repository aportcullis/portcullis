package audit_test

import (
	"context"
	"errors"
	"testing"

	auditapp "github.com/aportcullis/portcullis/internal/app/audit"
	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
)

type fakeReader struct {
	got     domainaudit.ListParams
	count   int64
	events  []domainaudit.Event
	listErr error
	cntErr  error
}

func (f *fakeReader) List(_ context.Context, p domainaudit.ListParams) ([]domainaudit.Event, error) {
	f.got = p
	return f.events, f.listErr
}

func (f *fakeReader) Count(context.Context) (int64, error) {
	return f.count, f.cntErr
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
		if page.PageSize != c.want || f.got.Limit != int64(c.want) {
			t.Errorf("page_size %d → echoed %d / limit %d, want %d", c.in, page.PageSize, f.got.Limit, c.want)
		}
	}
}

func TestListFloorsPageAndComputesOffset(t *testing.T) {
	f := &fakeReader{}
	page, err := newService(t, f).List(context.Background(), auditapp.Query{Page: 3, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Page != 3 || f.got.Offset != 40 { // (3-1)*20
		t.Errorf("page=%d offset=%d, want page 3 offset 40", page.Page, f.got.Offset)
	}

	f2 := &fakeReader{}
	page2, _ := newService(t, f2).List(context.Background(), auditapp.Query{Page: -5, PageSize: 20})
	if page2.Page != 1 || f2.got.Offset != 0 {
		t.Errorf("page<1 → page=%d offset=%d, want page 1 offset 0", page2.Page, f2.got.Offset)
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
	if _, err := newService(t, &fakeReader{cntErr: sentinel}).List(context.Background(), auditapp.Query{Page: 1, PageSize: 20}); !errors.Is(err, sentinel) {
		t.Fatalf("Count error not propagated: %v", err)
	}
	if _, err := newService(t, &fakeReader{listErr: sentinel}).List(context.Background(), auditapp.Query{Page: 1, PageSize: 20}); !errors.Is(err, sentinel) {
		t.Fatalf("List error not propagated: %v", err)
	}
}

func TestNewRejectsNilReader(t *testing.T) {
	if _, err := auditapp.New(nil); err == nil {
		t.Fatal("expected error for nil reader")
	}
}
