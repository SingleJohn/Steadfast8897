package database

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultSlowSQLThreshold = 300 * time.Millisecond

type queryTraceKey struct{}

type queryTraceData struct {
	sql   string
	start time.Time
}

type SlowSQLTracer struct {
	threshold time.Duration
}

func NewSlowSQLTracerFromEnv() *SlowSQLTracer {
	return &SlowSQLTracer{threshold: slowSQLThresholdFromEnv()}
}

func slowSQLThresholdFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("FYMS_SQL_SLOW_MS"))
	if raw == "" {
		return defaultSlowSQLThreshold
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return defaultSlowSQLThreshold
	}
	return time.Duration(ms) * time.Millisecond
}

func (t *SlowSQLTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryTraceKey{}, queryTraceData{
		sql:   data.SQL,
		start: time.Now(),
	})
}

func (t *SlowSQLTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	trace, ok := ctx.Value(queryTraceKey{}).(queryTraceData)
	if !ok {
		return
	}
	elapsed := time.Since(trace.start)
	if data.Err == nil && elapsed < t.threshold {
		return
	}

	attrs := []any{
		"log_target", "database",
		"sql", trace.sql,
		"elapsed_ms", elapsed.Milliseconds(),
	}
	if data.Err != nil {
		attrs = append(attrs, "error", data.Err)
		slog.Warn("SQL query failed", attrs...)
		return
	}
	slog.Warn("Slow SQL query", attrs...)
}
