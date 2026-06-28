package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Conversions between the sqlc/pgx wire types and plain Go values used by the
// domain (string ids, time.Time). Kept in one place so repositories stay clean.

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	v, err := u.Value()
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func stringToUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func tsToTime(t pgtype.Timestamptz) time.Time { return t.Time }

func timeToTS(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func tsToTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	tt := t.Time
	return &tt
}
