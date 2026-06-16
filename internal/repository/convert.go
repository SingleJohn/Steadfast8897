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
