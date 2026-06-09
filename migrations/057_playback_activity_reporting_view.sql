-- 057: Extend Playback Reporting compatibility fields.
DROP VIEW IF EXISTS "PlaybackActivity";
CREATE VIEW "PlaybackActivity" AS
SELECT
  id,
  date_created AS "DateCreated",
  user_id::text AS "UserId",
  item_id::text AS "ItemId",
  item_type AS "ItemType",
  item_name AS "ItemName",
  play_method AS "PlaybackMethod",
  client_name AS "ClientName",
  device_name AS "DeviceName",
  play_duration AS "PlayDuration",
  0 AS "PauseDuration",
  client_ip AS "ClientIp",
  client_ip AS "RemoteAddress",
  series_name AS "SeriesName"
FROM playback_activity;
