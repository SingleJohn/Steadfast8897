-- name: ListDisplayOrder :many
SELECT entry_id, sort_order FROM library_display_order;

-- name: ClearDisplayOrder :exec
DELETE FROM library_display_order;

-- name: UpsertDisplayOrderEntry :exec
INSERT INTO library_display_order (entry_kind, entry_id, sort_order)
VALUES ($1, $2, $3)
ON CONFLICT (entry_kind, entry_id) DO UPDATE SET sort_order = EXCLUDED.sort_order;
