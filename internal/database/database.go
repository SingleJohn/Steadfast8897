package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
)

func CreatePool(cfg *config.AppConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL())
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.DBPoolMax)
	poolCfg.MinConns = int32(cfg.DBPoolMin)
	poolCfg.MaxConnIdleTime = 30 * time.Second
	poolCfg.MaxConnLifetime = 5 * time.Minute
	poolCfg.ConnConfig.Tracer = NewSlowSQLTracerFromEnv()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

func RunMigrations(pool *pgxpool.Pool, migrationsDir string) error {
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL UNIQUE,
			applied_at TIMESTAMP NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		ALTER TABLE IF EXISTS migrations ADD COLUMN IF NOT EXISTS checksum VARCHAR(64);
		ALTER TABLE IF EXISTS migrations ADD COLUMN IF NOT EXISTS execution_time_ms BIGINT;
		ALTER TABLE IF EXISTS migrations ADD COLUMN IF NOT EXISTS error TEXT;
	`); err != nil {
		return fmt.Errorf("upgrade migrations table: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	rows, err := pool.Query(ctx, "SELECT name, checksum, error FROM migrations")
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	type migrationRecord struct {
		checksum string
		applied  bool
	}
	applied := make(map[string]migrationRecord)
	for rows.Next() {
		var name string
		var checksum *string
		var migrationErr *string
		if err := rows.Scan(&name, &checksum, &migrationErr); err != nil {
			return err
		}
		rec := migrationRecord{applied: migrationErr == nil}
		if checksum != nil {
			rec.checksum = *checksum
		}
		applied[name] = rec
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan applied migrations: %w", err)
	}

	for _, file := range files {
		sql, err := os.ReadFile(filepath.Join(migrationsDir, file))
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		checksum := checksumSQL(sql)

		if rec, ok := applied[file]; ok && rec.applied {
			if rec.checksum == "" {
				if _, err := pool.Exec(ctx, "UPDATE migrations SET checksum = $1 WHERE name = $2 AND (checksum IS NULL OR checksum = '')", checksum, file); err != nil {
					return fmt.Errorf("backfill checksum for %s: %w", file, err)
				}
				continue
			}
			if rec.checksum != checksum {
				return fmt.Errorf("migration %s checksum mismatch: applied=%s current=%s", file, rec.checksum, checksum)
			}
			continue
		}

		slog.Info("Applying migration", "file", file)

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", file, err)
		}

		start := time.Now()
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			elapsedMS := time.Since(start).Milliseconds()
			if recordErr := recordMigrationFailure(ctx, pool, file, checksum, elapsedMS, err); recordErr != nil {
				slog.Warn("Failed to record migration error", "file", file, "error", recordErr)
			}
			return fmt.Errorf("exec %s: %w", file, err)
		}
		elapsedMS := time.Since(start).Milliseconds()

		if _, err := tx.Exec(ctx, `
			INSERT INTO migrations (name, checksum, execution_time_ms, error)
			VALUES ($1, $2, $3, NULL)
			ON CONFLICT (name) DO UPDATE SET
				applied_at = NOW(),
				checksum = EXCLUDED.checksum,
				execution_time_ms = EXCLUDED.execution_time_ms,
				error = NULL
		`, file, checksum, elapsedMS); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", file, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", file, err)
		}

		slog.Info("Applied migration", "file", file)
	}

	slog.Info("All migrations applied")
	return nil
}

func checksumSQL(sql []byte) string {
	sum := sha256.Sum256(sql)
	return hex.EncodeToString(sum[:])
}

func recordMigrationFailure(ctx context.Context, pool *pgxpool.Pool, name, checksum string, executionTimeMS int64, migrationErr error) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO migrations (name, checksum, execution_time_ms, error)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE SET
			checksum = EXCLUDED.checksum,
			execution_time_ms = EXCLUDED.execution_time_ms,
			error = EXCLUDED.error
	`, name, checksum, executionTimeMS, migrationErr.Error())
	return err
}
