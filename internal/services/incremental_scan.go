package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FileChangeEvent struct {
	Action          string  `json:"action"`
	IsDir           bool    `json:"is_dir"`
	SourceFile      string  `json:"source_file"`
	DestinationFile *string `json:"destination_file,omitempty"`
}

func HandleFileChangeEvents(ctx context.Context, pool *pgxpool.Pool, cache *CacheService, events []FileChangeEvent) {
	mappings := getWebhookPathMappings(ctx, pool)

	for _, event := range events {
		mappedSource := applyWebhookPathMappings(event.SourceFile, mappings)
		var mappedDest *string
		if event.DestinationFile != nil {
			d := applyWebhookPathMappings(*event.DestinationFile, mappings)
			mappedDest = &d
		}

		var err error
		switch strings.ToLower(event.Action) {
		case "create", "add", "modify", "change":
			err = handleCreate(ctx, pool, mappedSource, event.IsDir)
		case "delete", "remove":
			err = handleDelete(ctx, pool, mappedSource, event.IsDir)
		case "rename", "move":
			if mappedDest != nil {
				err = handleRename(ctx, pool, mappedSource, *mappedDest, event.IsDir)
			}
		default:
			slog.Warn("[Webhook] Unknown action", "action", event.Action)
		}

		if err != nil {
			slog.Error("[Webhook] Error processing event", "error", err)
		}
	}

	cache.Del(ctx, "views:all")
	cache.DelPattern(ctx, "latest:*")
}

func getWebhookPathMappings(ctx context.Context, pool *pgxpool.Pool) [][2]string {
	var val *string
	pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'webhook_path_mappings'").Scan(&val)
	if val == nil {
		return nil
	}

	var arr []map[string]string
	if err := json.Unmarshal([]byte(*val), &arr); err != nil {
		return nil
	}

	var mappings [][2]string
	for _, m := range arr {
		from, ok1 := m["from"]
		to, ok2 := m["to"]
		if ok1 && ok2 {
			mappings = append(mappings, [2]string{from, to})
		}
	}
	return mappings
}

func applyWebhookPathMappings(path string, mappings [][2]string) string {
	for _, m := range mappings {
		if strings.HasPrefix(path, m[0]) {
			return m[1] + path[len(m[0]):]
		}
	}
	return path
}

func findLibraryForPath(ctx context.Context, pool *pgxpool.Pool, filePath string) (libID, collectionType string, found bool) {
	rows, err := pool.Query(ctx, "SELECT id, collection_type, paths FROM libraries")
	if err != nil {
		return "", "", false
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var ct string
		var paths []string
		if err := rows.Scan(&id, &ct, &paths); err != nil {
			continue
		}
		for _, lp := range paths {
			normalized := lp
			if !strings.HasSuffix(normalized, "/") {
				normalized += "/"
			}
			if strings.HasPrefix(filePath, normalized) || filePath == lp {
				return id.String(), ct, true
			}
		}
	}
	return "", "", false
}

func handleCreate(ctx context.Context, pool *pgxpool.Pool, filePath string, isDir bool) error {
	libID, collectionType, found := findLibraryForPath(ctx, pool, filePath)
	if !found {
		return nil
	}

	if collectionType == "movies" {
		return handleMovieCreate(ctx, pool, filePath, isDir, libID)
	}

	slog.Info("[Webhook] TV show file change", "path", filePath)
	return nil
}

func handleMovieCreate(ctx context.Context, pool *pgxpool.Pool, filePath string, isDir bool, libID string) error {
	if isDir {
		return nil
	}

	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	if !IsVideoExt("." + ext) {
		return nil
	}

	var existingID uuid.UUID
	err := pool.QueryRow(ctx, "SELECT id FROM items WHERE file_path = $1", filePath).Scan(&existingID)
	if err == nil {
		return nil
	}

	basename := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	parsed := ParseMovieName(basename)
	mi := ReadMediainfoJSON(filePath)
	var runtime *int64
	if mi != nil {
		if v, ok := mi["RunTimeTicks"]; ok {
			if f, ok := v.(float64); ok {
				r := int64(f)
				runtime = &r
			}
		}
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container, created_at)
		 VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7, COALESCE($8, NOW())) ON CONFLICT DO NOTHING`,
		libID, parsed.Name, strings.ToLower(parsed.Name), parsed.Year, runtime, filePath, ext, fileMtimeOrNil(filePath))
	if err != nil {
		return err
	}

	slog.Info("[Webhook] Movie file added", "name", parsed.Name)
	return nil
}

func handleDelete(ctx context.Context, pool *pgxpool.Pool, filePath string, isDir bool) error {
	if isDir {
		tag, err := pool.Exec(ctx, "DELETE FROM items WHERE file_path LIKE $1", filePath+"%")
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			slog.Info("[Webhook] Deleted items from directory", "count", tag.RowsAffected(), "path", filePath)
			return cleanupEmptyParents(ctx, pool)
		}
	} else {
		tag, err := pool.Exec(ctx, "DELETE FROM items WHERE file_path = $1", filePath)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			slog.Info("[Webhook] Deleted item", "path", filePath)
			return cleanupEmptyParents(ctx, pool)
		}
	}
	return nil
}

func handleRename(ctx context.Context, pool *pgxpool.Pool, oldPath, newPath string, isDir bool) error {
	if isDir {
		rows, err := pool.Query(ctx, "SELECT id, file_path FROM items WHERE file_path LIKE $1", oldPath+"%")
		if err != nil {
			return err
		}
		var updates []struct {
			id uuid.UUID
			fp string
		}
		for rows.Next() {
			var id uuid.UUID
			var fp string
			rows.Scan(&id, &fp)
			updates = append(updates, struct {
				id uuid.UUID
				fp string
			}{id, fp})
		}
		rows.Close()

		for _, u := range updates {
			updated := newPath + u.fp[len(oldPath):]
			pool.Exec(ctx, "UPDATE items SET file_path = $1, updated_at = NOW() WHERE id = $2::uuid", updated, u.id)
			pool.Exec(ctx, "UPDATE media_versions SET file_path = $1 WHERE file_path = $2", updated, u.fp)
		}
		if len(updates) > 0 {
			slog.Info("[Webhook] Renamed items", "count", len(updates), "from", oldPath, "to", newPath)
		}
	} else {
		tag, err := pool.Exec(ctx, "UPDATE items SET file_path = $1, updated_at = NOW() WHERE file_path = $2", newPath, oldPath)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			pool.Exec(ctx, "UPDATE media_versions SET file_path = $1 WHERE file_path = $2", newPath, oldPath)
			slog.Info("[Webhook] Renamed", "from", oldPath, "to", newPath)
		} else {
			ext := strings.TrimPrefix(filepath.Ext(newPath), ".")
			if IsVideoExt("." + ext) {
				return handleCreate(ctx, pool, newPath, false)
			}
		}
	}
	return nil
}

func cleanupEmptyParents(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM items WHERE type = 'Season' AND id NOT IN (
			SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Episode'
		) AND type = 'Season'`)
	if err != nil {
		return fmt.Errorf("cleanup seasons: %w", err)
	}

	_, err = pool.Exec(ctx,
		`DELETE FROM items WHERE type = 'Series' AND id NOT IN (
			SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Season'
		) AND type = 'Series'`)
	if err != nil {
		return fmt.Errorf("cleanup series: %w", err)
	}
	return nil
}
