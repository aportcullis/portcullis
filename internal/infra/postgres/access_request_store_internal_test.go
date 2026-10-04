package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

func TestWithReadSnapshotHidesConcurrentCommits(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewConnectionStore(pool)
	countOrgs := func(row pgx.Row) int {
		var n int
		if err := row.Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	const countSQL = `select count(*) from organizations`
	before := countOrgs(pool.QueryRow(ctx, countSQL))

	var first, second int
	err := store.withReadSnapshot(ctx, func(tx pgx.Tx, _ *db.Queries) error {
		first = countOrgs(tx.QueryRow(ctx, countSQL))

		if _, err := pool.Exec(ctx,
			`insert into organizations (name, slug) values ('Racy', 'racy-org')`); err != nil {
			return err
		}
		second = countOrgs(tx.QueryRow(ctx, countSQL))
		return nil
	})
	if err != nil {
		t.Fatalf("withReadSnapshot: %v", err)
	}
	if first != before || second != before {
		t.Errorf("reads inside one snapshot saw %d then %d, want %d both times — the second statement took a new snapshot",
			first, second, before)
	}

	if after := countOrgs(pool.QueryRow(ctx, countSQL)); after != before+1 {
		t.Errorf("after the snapshot the row count is %d, want %d", after, before+1)
	}
}

func TestWithReadSnapshotRefusesWrites(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewConnectionStore(pool)

	err := store.withReadSnapshot(ctx, func(tx pgx.Tx, _ *db.Queries) error {
		_, writeErr := tx.Exec(ctx, `insert into organizations (name, slug) values ('Nope', 'nope')`)
		return writeErr
	})
	if err == nil {
		t.Error("a write inside withReadSnapshot succeeded; the transaction is not read-only")
	}
}

// transitionAt is unexported and every exported store path passes a domain edge, so only a white-box call can offer it an edge outside the state machine.
func TestTransitionRefusesEdgesOutsideTheStateMachine(t *testing.T) {
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewAccessRequestStore(pool)
	missingRequest, err := stringToUUID(uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	organization, err := stringToUUID(uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	queries := db.New(pool)

	for _, edge := range []struct{ from, to access.State }{
		{access.StateSucceeded, access.StateApproved},
		{access.StatePending, access.StateExecuting},
		{access.StateApproved, access.StatePending},
		{access.StateDraft, access.StateSucceeded},
		{access.StateExpired, access.StateApproved},
	} {
		_, err := store.transitionAt(ctx, queries, missingRequest, organization, edge.from, edge.to, nil, nil, time.Time{})
		if !errors.Is(err, access.ErrInvalidTransition) {
			t.Errorf("transition %s → %s = %v, want ErrInvalidTransition before the conditional update", edge.from, edge.to, err)
		}
	}

	// Domain edges reach the conditional update, which finds no row and reports a lost guard.
	for _, edge := range []struct{ from, to access.State }{
		{access.StatePending, access.StateApproved},
		{access.StateApproved, access.StateExecuting},
		{access.StateExecuting, access.StateOutcomeUnknown},
		{access.StateDraft, access.StateCancelled},
	} {
		_, err := store.transitionAt(ctx, queries, missingRequest, organization, edge.from, edge.to, nil, nil, time.Time{})
		if !errors.Is(err, access.ErrConflict) || errors.Is(err, access.ErrInvalidTransition) {
			t.Errorf("transition %s → %s on a missing row = %v, want ErrConflict from the guard", edge.from, edge.to, err)
		}
	}
}

func TestRowCoreCopiesEveryColumn(t *testing.T) {
	var vr requestViewRow
	fillNonZero(reflect.ValueOf(&vr).Elem())

	got := toBaseRequestRow(vr)

	rv := reflect.ValueOf(got)
	rt := rv.Type()
	for fieldIdx := range rv.NumField() {
		if rv.Field(fieldIdx).IsZero() {
			t.Errorf("toBaseRequestRow dropped db.AccessRequest field %q — add it to toBaseRequestRow", rt.Field(fieldIdx).Name)
		}
	}
}

func fillNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:

		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillNonZero(v.Index(0))
	case reflect.Array:
		if v.Len() > 0 {
			fillNonZero(v.Index(0))
		}
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(v.Elem())
	case reflect.Struct:
		for fieldIdx := range v.NumField() {
			f := v.Field(fieldIdx)
			if f.CanSet() {
				fillNonZero(f)
			}
		}
	}
}
