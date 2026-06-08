-- Emby-compatible outbound webhook notifications.

ALTER TABLE items ADD COLUMN IF NOT EXISTS library_new_notified_at TIMESTAMPTZ;

-- Treat pre-existing rows as already notified. Only new rows after this migration
-- are candidates for library.new notifications.
UPDATE items
   SET library_new_notified_at = NOW()
 WHERE library_new_notified_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_items_library_new_pending
    ON items (updated_at, library_id)
    WHERE library_new_notified_at IS NULL
      AND type IN ('Movie', 'Episode', 'Series');

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         VARCHAR(128) NOT NULL,
    url          TEXT NOT NULL,
    events       TEXT[] NOT NULL DEFAULT ARRAY['library.new', 'library.deleted'],
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    group_items  BOOLEAN NOT NULL DEFAULT FALSE,
    last_status  INTEGER,
    last_error   TEXT,
    last_sent_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_subscriptions_enabled
    ON webhook_subscriptions (enabled)
    WHERE enabled = TRUE;
