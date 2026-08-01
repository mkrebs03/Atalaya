CREATE TABLE service_checks (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    related_host_id TEXT REFERENCES hosts(id),
    type TEXT NOT NULL CHECK(type IN ('http', 'tcp')),
    target_url TEXT,
    target_host TEXT,
    target_port INTEGER,
    interval_seconds INTEGER NOT NULL CHECK(interval_seconds >= 15 AND interval_seconds <= 3600),
    timeout_seconds INTEGER NOT NULL CHECK(timeout_seconds >= 1 AND timeout_seconds <= 60 AND timeout_seconds < interval_seconds),
    expected_status_min INTEGER,
    expected_status_max INTEGER,
    follow_redirects INTEGER,
    max_redirects INTEGER,
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    observed_state TEXT NOT NULL CHECK(observed_state IN ('unknown', 'up', 'down')),
    last_attempt_at TEXT,
    last_result_at TEXT,
    freshness_deadline_at TEXT,
    consecutive_failures INTEGER NOT NULL CHECK(consecutive_failures >= 0),
    last_latency_ms INTEGER CHECK(last_latency_ms IS NULL OR last_latency_ms >= 0),
    last_error_code TEXT CHECK(last_error_code IS NULL OR last_error_code IN ('dns', 'timeout', 'connect', 'tls', 'http_status', 'invalid_target', 'internal')),
    last_http_status INTEGER CHECK(last_http_status IS NULL OR last_http_status >= 100 AND last_http_status <= 599),
    archived_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    configuration_generation INTEGER NOT NULL CHECK(configuration_generation >= 1),
    version INTEGER NOT NULL CHECK(version > 0),
    CHECK((type = 'http' AND target_url IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND expected_status_min IS NOT NULL AND expected_status_max IS NOT NULL AND follow_redirects IS NOT NULL AND max_redirects IS NOT NULL) OR (type = 'tcp' AND target_url IS NULL AND target_host IS NOT NULL AND target_port IS NOT NULL AND expected_status_min IS NULL AND expected_status_max IS NULL AND follow_redirects IS NULL AND max_redirects IS NULL)),
    CHECK(expected_status_min IS NULL OR expected_status_min >= 100 AND expected_status_min <= 599),
    CHECK(expected_status_max IS NULL OR expected_status_max >= expected_status_min AND expected_status_max <= 599),
    CHECK(follow_redirects IS NULL OR follow_redirects IN (0, 1)),
    CHECK(max_redirects IS NULL OR max_redirects >= 1 AND max_redirects <= 3)
);

CREATE INDEX service_checks_list ON service_checks (name COLLATE NOCASE, id);
CREATE INDEX service_checks_schedulable ON service_checks (enabled, archived_at);

CREATE TABLE check_results (
    check_id TEXT NOT NULL REFERENCES service_checks(id),
    slot_id TEXT NOT NULL REFERENCES schedule_slots(id),
    received_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('up', 'down')),
    latency_ms INTEGER CHECK(latency_ms IS NULL OR latency_ms >= 0),
    error_code TEXT CHECK(error_code IS NULL OR error_code IN ('dns', 'timeout', 'connect', 'tls', 'http_status', 'invalid_target', 'internal')),
    http_status INTEGER CHECK(http_status IS NULL OR http_status >= 100 AND http_status <= 599),
    UNIQUE(check_id, slot_id),
    CHECK(status = 'up' OR latency_ms IS NULL)
);

CREATE INDEX check_results_series_time ON check_results (check_id, received_at, slot_id);
CREATE INDEX check_results_slot ON check_results (slot_id);

CREATE TABLE check_hourly_rollups (
    check_id TEXT NOT NULL REFERENCES service_checks(id),
    metric_key TEXT NOT NULL CHECK(metric_key IN ('availability', 'latency_ms')),
    bucket_start TEXT NOT NULL,
    bucket_end TEXT NOT NULL,
    min_value REAL NOT NULL,
    max_value REAL NOT NULL,
    average_value REAL NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    successful_count INTEGER,
    expected_sample_count INTEGER,
    covered_slot_count INTEGER,
    complete INTEGER NOT NULL CHECK(complete IN (0, 1)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(check_id, metric_key, bucket_start),
    CHECK(bucket_end > bucket_start),
    CHECK(metric_key = 'availability' OR successful_count IS NULL),
    CHECK(metric_key = 'availability' OR expected_sample_count IS NULL AND covered_slot_count IS NULL),
    CHECK(successful_count IS NULL OR successful_count >= 0),
    CHECK(expected_sample_count IS NULL OR expected_sample_count >= 0),
    CHECK(covered_slot_count IS NULL OR covered_slot_count >= 0)
);

CREATE INDEX check_hourly_rollups_series_time ON check_hourly_rollups (check_id, metric_key, bucket_start);

CREATE TABLE check_runs (
    id TEXT PRIMARY KEY NOT NULL,
    check_id TEXT NOT NULL REFERENCES service_checks(id),
    request_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('queued', 'running', 'completed', 'failed')),
    requested_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    lease_until TEXT,
    process_attempts INTEGER NOT NULL CHECK(process_attempts >= 0 AND process_attempts <= 2),
    snapshot_type TEXT NOT NULL CHECK(snapshot_type IN ('http', 'tcp')),
    snapshot_url TEXT,
    snapshot_host TEXT,
    snapshot_port INTEGER,
    snapshot_interval_seconds INTEGER NOT NULL CHECK(snapshot_interval_seconds >= 15 AND snapshot_interval_seconds <= 3600),
    snapshot_timeout_seconds INTEGER NOT NULL CHECK(snapshot_timeout_seconds >= 1 AND snapshot_timeout_seconds <= 60 AND snapshot_timeout_seconds < snapshot_interval_seconds),
    snapshot_expected_status_min INTEGER,
    snapshot_expected_status_max INTEGER,
    snapshot_follow_redirects INTEGER,
    snapshot_max_redirects INTEGER,
    result_status TEXT CHECK(result_status IS NULL OR result_status IN ('up', 'down')),
    result_latency_ms INTEGER CHECK(result_latency_ms IS NULL OR result_latency_ms >= 0),
    result_error_code TEXT CHECK(result_error_code IS NULL OR result_error_code IN ('dns', 'timeout', 'connect', 'tls', 'http_status', 'invalid_target', 'internal')),
    result_http_status INTEGER CHECK(result_http_status IS NULL OR result_http_status >= 100 AND result_http_status <= 599),
    failure_code TEXT CHECK(failure_code IS NULL OR failure_code IN ('internal', 'retry_exhausted', 'check_archived')),
    UNIQUE(check_id, request_id),
    CHECK((snapshot_type = 'http' AND snapshot_url IS NOT NULL AND snapshot_host IS NULL AND snapshot_port IS NULL AND snapshot_expected_status_min IS NOT NULL AND snapshot_expected_status_max IS NOT NULL AND snapshot_follow_redirects IS NOT NULL AND snapshot_max_redirects IS NOT NULL) OR (snapshot_type = 'tcp' AND snapshot_url IS NULL AND snapshot_host IS NOT NULL AND snapshot_port IS NOT NULL AND snapshot_expected_status_min IS NULL AND snapshot_expected_status_max IS NULL AND snapshot_follow_redirects IS NULL AND snapshot_max_redirects IS NULL)),
    CHECK((state = 'running') = (lease_until IS NOT NULL)),
    CHECK((state IN ('completed', 'failed')) = (completed_at IS NOT NULL)),
    CHECK(state = 'completed' OR result_status IS NULL)
);

CREATE INDEX check_runs_queue ON check_runs (state, lease_until, requested_at);
CREATE INDEX check_runs_check_time ON check_runs (check_id, requested_at, id);

CREATE TABLE check_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    interval_seconds INTEGER NOT NULL CHECK(interval_seconds >= 15 AND interval_seconds <= 3600),
    timeout_seconds INTEGER NOT NULL CHECK(timeout_seconds >= 1 AND timeout_seconds <= 60 AND timeout_seconds < interval_seconds),
    allowed_private_cidrs TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO check_settings(id, interval_seconds, timeout_seconds, allowed_private_cidrs, updated_at)
VALUES(1, 60, 10, '[]', '1970-01-01T00:00:00Z');

CREATE TRIGGER schedule_periods_check_target_insert
BEFORE INSERT ON schedule_periods WHEN NEW.target_type = 'check'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM service_checks WHERE id = NEW.target_id
    ) THEN RAISE(ABORT, 'invalid check schedule target') END;
END;

CREATE TRIGGER schedule_periods_check_target_update
BEFORE UPDATE OF target_type, target_id ON schedule_periods WHEN NEW.target_type = 'check'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM service_checks WHERE id = NEW.target_id
    ) THEN RAISE(ABORT, 'invalid check schedule target') END;
END;

CREATE TRIGGER schedule_slots_check_target_insert
BEFORE INSERT ON schedule_slots WHEN NEW.target_type = 'check'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM service_checks WHERE id = NEW.target_id
    ) THEN RAISE(ABORT, 'invalid check schedule target') END;
END;

CREATE TRIGGER schedule_slots_check_target_update
BEFORE UPDATE OF target_type, target_id ON schedule_slots WHEN NEW.target_type = 'check'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM service_checks WHERE id = NEW.target_id
    ) THEN RAISE(ABORT, 'invalid check schedule target') END;
END;
