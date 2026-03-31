INSERT INTO system_config (key, value) VALUES ('scan_threads', '3') ON CONFLICT (key) DO NOTHING;
