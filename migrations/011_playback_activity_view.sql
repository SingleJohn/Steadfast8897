-- Emby Playback Reporting compatibility view
-- Sakura_embyboss and other tools query "PlaybackActivity" with Emby column names
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
  client_ip AS "ClientIp",
  series_name AS "SeriesName"
FROM playback_activity;
