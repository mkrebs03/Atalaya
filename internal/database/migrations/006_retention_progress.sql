ALTER TABLE retention_progress ADD COLUMN last_raw_retention_seconds INTEGER;
ALTER TABLE retention_progress ADD COLUMN last_rollup_retention_seconds INTEGER;

UPDATE retention_progress
SET last_raw_retention_seconds = (SELECT raw_retention_seconds FROM retention_settings WHERE id = 1),
    last_rollup_retention_seconds = (SELECT rollup_retention_seconds FROM retention_settings WHERE id = 1)
WHERE id = 1;
