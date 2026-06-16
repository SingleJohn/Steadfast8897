package repository

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func toPGUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}

func fromPGUUID(id pgtype.UUID) uuid.UUID {
	return uuid.UUID(id.Bytes)
}

func ptrTime(v pgtype.Timestamp) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func ptrText(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func ptrInt32(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	i := v.Int32
	return &i
}

func textValue(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

func nullableText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func nullableInt32(v int) pgtype.Int4 {
	if v == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(v), Valid: true}
}

func nullableBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}

func intervalFromDuration(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

func ptrInt32FromPG(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	i := v.Int32
	return &i
}

func ptrIntFromPG(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int32)
	return &i
}

func ptrTextFromPG(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func optionalScrapeConfig(v any) *string {
	switch raw := v.(type) {
	case nil:
		return nil
	case string:
		if raw == "" {
			return nil
		}
		return &raw
	case []byte:
		s := string(raw)
		if s == "" {
			return nil
		}
		return &s
	default:
		s := fmt.Sprint(raw)
		if s == "" || s == "<nil>" {
			return nil
		}
		return &s
	}
}
