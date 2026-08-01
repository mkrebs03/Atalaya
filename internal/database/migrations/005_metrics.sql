CREATE TABLE metric_samples (
    host_id TEXT NOT NULL REFERENCES hosts(id),
    scope_id TEXT NOT NULL,
    filesystem_id TEXT REFERENCES filesystems(id),
    metric_key TEXT NOT NULL CHECK(metric_key IN ('cpu_used_percent', 'memory_used_bytes', 'memory_total_bytes', 'filesystem_used_bytes', 'filesystem_total_bytes')),
    slot_id TEXT NOT NULL REFERENCES schedule_slots(id),
    received_at TEXT NOT NULL,
    value_real REAL,
    value_integer INTEGER,
    CHECK((value_real IS NULL) != (value_integer IS NULL)),
    UNIQUE(scope_id, metric_key, slot_id)
);

CREATE INDEX metric_samples_series_time
ON metric_samples (scope_id, metric_key, received_at, slot_id);

CREATE INDEX metric_samples_slot
ON metric_samples (slot_id);

CREATE TABLE hourly_rollups (
    host_id TEXT NOT NULL REFERENCES hosts(id),
    scope_id TEXT NOT NULL,
    filesystem_id TEXT REFERENCES filesystems(id),
    metric_key TEXT NOT NULL CHECK(metric_key IN ('cpu_used_percent', 'memory_used_bytes', 'memory_total_bytes', 'filesystem_used_bytes', 'filesystem_total_bytes')),
    bucket_start TEXT NOT NULL,
    bucket_end TEXT NOT NULL,
    min_value REAL NOT NULL,
    max_value REAL NOT NULL,
    average_value REAL NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    expected_sample_count INTEGER,
    covered_slot_count INTEGER,
    complete INTEGER NOT NULL CHECK(complete IN (0, 1)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(scope_id, metric_key, bucket_start),
    CHECK(bucket_end > bucket_start),
    CHECK(expected_sample_count IS NULL OR expected_sample_count >= 0),
    CHECK(covered_slot_count IS NULL OR covered_slot_count >= 0)
);

CREATE INDEX hourly_rollups_series_time
ON hourly_rollups (scope_id, metric_key, bucket_start);

CREATE TABLE retention_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    raw_retention_seconds INTEGER NOT NULL CHECK(raw_retention_seconds >= 86400 AND raw_retention_seconds <= 2592000),
    rollup_retention_seconds INTEGER NOT NULL CHECK(rollup_retention_seconds >= 604800 AND rollup_retention_seconds <= 63072000),
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL CHECK(version > 0),
    CHECK(rollup_retention_seconds >= raw_retention_seconds)
);

INSERT INTO retention_settings(id,raw_retention_seconds,rollup_retention_seconds,updated_at,version)
VALUES(1,172800,7776000,'1970-01-01T00:00:00Z',1);

CREATE TABLE retention_progress (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    last_rollup_bucket_start TEXT,
    last_raw_cleanup_at TEXT,
    updated_at TEXT NOT NULL
);

INSERT INTO retention_progress(id,updated_at)
VALUES(1,'1970-01-01T00:00:00Z');
