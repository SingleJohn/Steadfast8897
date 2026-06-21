package repository

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var pgPlaceholderPattern = regexp.MustCompile(`\$(\d+)`)

func (r *SourceRepository) DeleteExpiredSourceItems(ctx context.Context, cutoff time.Time, limit int64) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("source repository is nil")
	}
	if limit <= 0 {
		limit = 500
	}
	args := []any{cutoff, limit}
	clauses := []string{"si.last_seen_at < $1"}

	views, err := r.ListExposedLibraryViews(ctx)
	if err != nil {
		return 0, err
	}
	protected := make([]string, 0, len(views))
	for _, view := range views {
		where, whereArgs, err := sourceViewWhere(view, nil)
		if err != nil {
			return 0, fmt.Errorf("source view %d filter invalid: %w", view.ID, err)
		}
		protected = append(protected, "("+shiftPGPlaceholders(where, len(args))+")")
		args = append(args, whereArgs...)
	}
	if len(protected) > 0 {
		clauses = append(clauses, "NOT ("+strings.Join(protected, " OR ")+")")
	}

	var deleted int64
	err = r.pool.QueryRow(ctx, `
		WITH expired AS (
			SELECT si.id
			  FROM source_items si
			 WHERE `+strings.Join(clauses, " AND ")+`
			 ORDER BY si.last_seen_at ASC, si.id ASC
			 LIMIT $2
		),
		deleted AS (
			DELETE FROM source_items si
			 USING expired
			 WHERE si.id = expired.id
			 RETURNING si.id
		)
		SELECT COUNT(*) FROM deleted`, args...).Scan(&deleted)
	return deleted, err
}

func shiftPGPlaceholders(sql string, offset int) string {
	if offset == 0 {
		return sql
	}
	return pgPlaceholderPattern.ReplaceAllStringFunc(sql, func(match string) string {
		n, err := strconv.Atoi(strings.TrimPrefix(match, "$"))
		if err != nil {
			return match
		}
		return "$" + strconv.Itoa(n+offset)
	})
}
