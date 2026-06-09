package models

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MergeMultiVersionItems merges duplicate items WITHIN the same platform
// (studio) so that each platform virtual library shows only one entry per
// logical movie, with all physical versions aggregated as MediaSources.
//
// Key design decisions (learned from Jellyfin source):
//   - Group by ANY shared external_id (tmdb / imdb / douban / bangumi / ...) within
//     the same studio — never merge across different studios. Multi-source primary
//     IDs are treated equally; a transitive closure (union-find) bridges items that
//     share different IDs (e.g. A↔B via tmdb, A↔C via imdb → {A,B,C} one group).
//   - Only merge Movies (Series require episode re-parenting which is complex)
//   - Reset all previous merges first to ensure idempotent results
//   - The merged_to_id filter is only applied in platform library queries,
//     so regular library browsing remains unaffected
func MergeMultiVersionItems(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	// Full reset: undo all previous merges so we can re-compute cleanly.
	// This ensures idempotent behavior and fixes any stale/incorrect merges.
	resetTag, _ := pool.Exec(ctx, `UPDATE items SET merged_to_id = NULL WHERE merged_to_id IS NOT NULL`)
	if resetTag.RowsAffected() > 0 {
		slog.Info("[Merge] Reset previous merges", "reset_count", resetTag.RowsAffected())
	}

	// Pull all (item_id, studio, provider, external_id) for unmerged Movies in
	// a platform library context. Joining item_external_ids supports multi-source
	// primary IDs; one item may appear multiple rows if it has several external IDs.
	rows, err := pool.Query(ctx, `
		SELECT i.id::text, i.studio, e.provider, e.external_id
		  FROM items i
		  JOIN item_external_ids e ON e.item_id = i.id
		 WHERE i.type = 'Movie'
		   AND i.merged_to_id IS NULL
		   AND i.studio IS NOT NULL AND i.studio <> ''`)
	if err != nil {
		return 0, fmt.Errorf("find merge candidates: %w", err)
	}
	defer rows.Close()

	// keyToItems: provider:external_id:studio → [item_id, ...]
	keyToItems := map[string][]string{}
	for rows.Next() {
		var itemID, studio, provider, externalID string
		if err := rows.Scan(&itemID, &studio, &provider, &externalID); err != nil {
			return 0, err
		}
		key := provider + ":" + externalID + ":" + studio
		keyToItems[key] = append(keyToItems[key], itemID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// Union-find across all keys to handle transitive closure.
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if _, ok := parent[x]; !ok {
			parent[x] = x
			return x
		}
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(x, y string) {
		rx, ry := find(x), find(y)
		if rx != ry {
			parent[rx] = ry
		}
	}
	for _, items := range keyToItems {
		for i := 1; i < len(items); i++ {
			union(items[0], items[i])
		}
	}

	// Cluster items by root (union-find component).
	rootToMembers := map[string][]string{}
	for itemID := range parent {
		root := find(itemID)
		rootToMembers[root] = append(rootToMembers[root], itemID)
	}

	groupCount := 0
	for _, members := range rootToMembers {
		if len(members) >= 2 {
			groupCount++
		}
	}
	slog.Info("[Merge] Found duplicate groups", "count", groupCount)

	merged := 0
	for _, members := range rootToMembers {
		if len(members) < 2 {
			continue
		}
		// Pick best primary among members: image > overview > most recent.
		var primaryID string
		err := pool.QueryRow(ctx,
			`SELECT id::text FROM items
			  WHERE id = ANY($1::uuid[]) AND merged_to_id IS NULL
			  ORDER BY
			    (CASE WHEN primary_image_tag IS NOT NULL THEN 0 ELSE 1 END),
			    (CASE WHEN primary_image_path IS NOT NULL AND primary_image_path <> '' THEN 0 ELSE 1 END),
			    (CASE WHEN overview IS NOT NULL AND overview <> '' THEN 0 ELSE 1 END),
			    updated_at DESC
			  LIMIT 1`, members).Scan(&primaryID)
		if err != nil {
			slog.Warn("[Merge] Failed to pick primary", "members", members, "error", err)
			continue
		}

		others := make([]string, 0, len(members)-1)
		for _, m := range members {
			if m != primaryID {
				others = append(others, m)
			}
		}
		if len(others) == 0 {
			continue
		}
		tag, err := pool.Exec(ctx,
			`UPDATE items SET merged_to_id = $1::uuid
			  WHERE id = ANY($2::uuid[]) AND merged_to_id IS NULL`,
			primaryID, others)
		if err != nil {
			slog.Warn("[Merge] Failed to set merged_to_id", "primary", primaryID, "error", err)
			continue
		}
		merged += int(tag.RowsAffected())

		syncBestMetadataToPrimary(ctx, pool, primaryID)
	}

	// Re-sync metadata for already-merged groups where primary still lacks data
	syncExistingMergedGroups(ctx, pool)

	return merged, nil
}

// syncBestMetadataToPrimary fills NULL/empty metadata fields on the primary
// using the best available value from any group member (primary itself or
// any item whose merged_to_id points to primary).
func syncBestMetadataToPrimary(ctx context.Context, pool *pgxpool.Pool, primaryID string) {
	_, err := pool.Exec(ctx, `
		UPDATE items p SET
			primary_image_path  = COALESCE(NULLIF(p.primary_image_path, ''),  best.img_path),
			primary_image_tag   = COALESCE(NULLIF(p.primary_image_tag, ''),   best.img_tag),
			backdrop_image_path = COALESCE(NULLIF(p.backdrop_image_path, ''), best.bd_path),
			backdrop_image_tag  = COALESCE(NULLIF(p.backdrop_image_tag, ''),  best.bd_tag),
			overview            = COALESCE(NULLIF(p.overview, ''),            best.overview),
			community_rating    = COALESCE(p.community_rating,                best.rating),
			official_rating     = COALESCE(NULLIF(p.official_rating, ''),     best.official)
		FROM (
			SELECT
				(SELECT primary_image_path  FROM items WHERE (id = $1::uuid OR merged_to_id = $1::uuid) AND primary_image_path  IS NOT NULL AND primary_image_path  <> '' LIMIT 1) AS img_path,
				(SELECT primary_image_tag   FROM items WHERE (id = $1::uuid OR merged_to_id = $1::uuid) AND primary_image_tag   IS NOT NULL AND primary_image_tag   <> '' LIMIT 1) AS img_tag,
				(SELECT backdrop_image_path FROM items WHERE (id = $1::uuid OR merged_to_id = $1::uuid) AND backdrop_image_path IS NOT NULL AND backdrop_image_path <> '' LIMIT 1) AS bd_path,
				(SELECT backdrop_image_tag  FROM items WHERE (id = $1::uuid OR merged_to_id = $1::uuid) AND backdrop_image_tag  IS NOT NULL AND backdrop_image_tag  <> '' LIMIT 1) AS bd_tag,
				(SELECT overview            FROM items WHERE (id = $1::uuid OR merged_to_id = $1::uuid) AND overview IS NOT NULL AND overview <> '' LIMIT 1) AS overview,
				(SELECT community_rating    FROM items WHERE (id = $1::uuid OR merged_to_id = $1::uuid) AND community_rating    IS NOT NULL LIMIT 1) AS rating,
				(SELECT official_rating     FROM items WHERE (id = $1::uuid OR merged_to_id = $1::uuid) AND official_rating     IS NOT NULL AND official_rating <> '' LIMIT 1) AS official
		) best
		WHERE p.id = $1::uuid`,
		primaryID)
	if err != nil {
		slog.Warn("[Merge] syncBestMetadata failed", "primary", primaryID, "error", err)
	}
}

// syncExistingMergedGroups re-syncs metadata for primaries that already
// have secondaries but still lack some metadata fields.
func syncExistingMergedGroups(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT p.id::text
		   FROM items p
		  WHERE p.merged_to_id IS NULL
		    AND EXISTS (SELECT 1 FROM items s WHERE s.merged_to_id = p.id)
		    AND (p.primary_image_tag IS NULL OR p.primary_image_tag = ''
		      OR p.backdrop_image_tag IS NULL OR p.backdrop_image_tag = ''
		      OR p.overview IS NULL OR p.overview = ''
		      OR p.community_rating IS NULL)`)
	if err != nil {
		return
	}
	defer rows.Close()

	var primaryIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		primaryIDs = append(primaryIDs, id)
	}
	for _, id := range primaryIDs {
		syncBestMetadataToPrimary(ctx, pool, id)
	}
}

// GetMediaSourceCount returns the total number of media_versions for an item,
// including versions from all items merged into it (via merged_to_id).
// Mirrors Jellyfin's Video.MediaSourceCount property which counts
// LinkedAlternateVersions + LocalAlternateVersions + 1.
func GetMediaSourceCount(ctx context.Context, pool *pgxpool.Pool, itemID string) int32 {
	var count int32
	pool.QueryRow(ctx,
		`SELECT COALESCE(
			(SELECT COUNT(*) FROM media_versions WHERE item_id = $1::uuid) +
			(SELECT COUNT(*) FROM media_versions mv
			   JOIN items s ON mv.item_id = s.id
			  WHERE s.merged_to_id = $1::uuid),
		0)`, itemID).Scan(&count)
	if count == 0 {
		count = 1
	}
	return count
}

// UnmergeItem resets merged_to_id for a specific item (manual unmerge).
func UnmergeItem(ctx context.Context, pool *pgxpool.Pool, itemID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE items SET merged_to_id = NULL WHERE id = $1::uuid OR merged_to_id = $1::uuid`, itemID)
	return err
}
