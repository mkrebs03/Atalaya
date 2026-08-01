CREATE TABLE alert_rules (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('host_unreachable', 'check_failed', 'host_metric_threshold', 'filesystem_threshold')),
    severity TEXT NOT NULL CHECK(severity IN ('warning', 'critical')),
    host_id TEXT REFERENCES hosts(id),
    check_id TEXT REFERENCES service_checks(id),
    filesystem_id TEXT REFERENCES filesystems(id),
    check_mode TEXT CHECK(check_mode IS NULL OR check_mode IN ('duration', 'attempts')),
    for_seconds INTEGER NOT NULL CHECK(for_seconds >= 0 AND for_seconds <= 86400),
    consecutive_failures INTEGER CHECK(consecutive_failures IS NULL OR consecutive_failures >= 1),
    metric TEXT CHECK(metric IS NULL OR metric IN ('cpu_used_percent', 'memory_used_percent')),
    operator TEXT CHECK(operator IS NULL OR operator IN ('gt', 'gte')),
    threshold REAL CHECK(threshold IS NULL OR threshold >= 0 AND threshold <= 100),
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    archived_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL CHECK(version > 0),
    CHECK(
        (type = 'host_unreachable' AND host_id IS NOT NULL AND check_id IS NULL AND filesystem_id IS NULL AND check_mode IS NULL AND consecutive_failures IS NULL AND metric IS NULL AND operator IS NULL AND threshold IS NULL)
        OR (type = 'check_failed' AND host_id IS NULL AND check_id IS NOT NULL AND filesystem_id IS NULL AND check_mode IS NOT NULL AND ((check_mode = 'duration' AND consecutive_failures IS NULL) OR (check_mode = 'attempts' AND consecutive_failures IS NOT NULL AND for_seconds = 0)) AND metric IS NULL AND operator IS NULL AND threshold IS NULL)
        OR (type = 'host_metric_threshold' AND host_id IS NOT NULL AND check_id IS NULL AND filesystem_id IS NULL AND check_mode IS NULL AND consecutive_failures IS NULL AND metric IS NOT NULL AND operator IS NOT NULL AND threshold IS NOT NULL)
        OR (type = 'filesystem_threshold' AND host_id IS NULL AND check_id IS NULL AND filesystem_id IS NOT NULL AND check_mode IS NULL AND consecutive_failures IS NULL AND metric IS NULL AND operator IS NOT NULL AND threshold IS NOT NULL)
    )
);

CREATE INDEX alert_rules_list ON alert_rules (name COLLATE NOCASE, id);
CREATE INDEX alert_rules_target ON alert_rules (type, host_id, check_id, filesystem_id, archived_at);
CREATE INDEX alert_rules_enabled ON alert_rules (enabled, archived_at);

CREATE TABLE alert_rule_channels (
    rule_id TEXT NOT NULL REFERENCES alert_rules(id),
    channel_id TEXT NOT NULL REFERENCES notification_channels(id),
    PRIMARY KEY(rule_id, channel_id)
);

CREATE TABLE alert_evaluations (
    rule_id TEXT PRIMARY KEY NOT NULL REFERENCES alert_rules(id),
    state TEXT NOT NULL CHECK(state IN ('inactive', 'pending', 'firing')),
    first_true_at TEXT,
    last_evaluated_at TEXT,
    last_value REAL,
    last_condition_status TEXT NOT NULL CHECK(last_condition_status IN ('true', 'false', 'unknown')),
    incident_id TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE incidents (
    id TEXT PRIMARY KEY NOT NULL,
    rule_id TEXT NOT NULL REFERENCES alert_rules(id),
    rule_name TEXT NOT NULL,
    rule_type TEXT NOT NULL CHECK(rule_type IN ('host_unreachable', 'check_failed', 'host_metric_threshold', 'filesystem_threshold')),
    target_id TEXT NOT NULL,
    target_name TEXT NOT NULL,
    severity TEXT NOT NULL CHECK(severity IN ('warning', 'critical')),
    state TEXT NOT NULL CHECK(state IN ('open', 'resolved')),
    condition_status TEXT NOT NULL CHECK(condition_status IN ('true', 'false', 'unknown')),
    started_at TEXT NOT NULL,
    fired_at TEXT NOT NULL,
    resolved_at TEXT,
    initial_value REAL,
    last_value REAL,
    resolution_reason TEXT,
    UNIQUE(rule_id, id)
);

CREATE UNIQUE INDEX incidents_one_open_per_rule ON incidents(rule_id) WHERE state = 'open';
CREATE INDEX incidents_list ON incidents (state, severity, fired_at DESC, id DESC);

CREATE TABLE incident_channel_snapshots (
    incident_id TEXT NOT NULL REFERENCES incidents(id),
    channel_id TEXT NOT NULL REFERENCES notification_channels(id),
    PRIMARY KEY(incident_id, channel_id)
);

CREATE TABLE notification_channels (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL CHECK(provider IN ('discord')),
    encrypted_config BLOB NOT NULL,
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    archived_at TEXT,
    last_test_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL CHECK(version > 0)
);

CREATE INDEX notification_channels_list ON notification_channels (name COLLATE NOCASE, id);

CREATE TABLE notification_deliveries (
    id TEXT PRIMARY KEY NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('incident', 'test')),
    channel_id TEXT NOT NULL REFERENCES notification_channels(id),
    incident_id TEXT REFERENCES incidents(id),
    transition TEXT CHECK(transition IS NULL OR transition IN ('firing', 'resolved')),
    test_request_id TEXT,
    payload TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('pending', 'sending', 'delivered', 'failed', 'skipped')),
    attempts INTEGER NOT NULL CHECK(attempts >= 0 AND attempts <= 6),
    next_attempt_at TEXT NOT NULL,
    lease_until TEXT,
    last_error_code TEXT,
    skipped_reason TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    delivered_at TEXT,
    CHECK((kind = 'incident' AND incident_id IS NOT NULL AND transition IS NOT NULL AND test_request_id IS NULL) OR (kind = 'test' AND incident_id IS NULL AND transition IS NULL AND test_request_id IS NOT NULL)),
    CHECK((state = 'sending') = (lease_until IS NOT NULL))
);

CREATE UNIQUE INDEX notification_deliveries_incident_unique
ON notification_deliveries (incident_id, transition, channel_id)
WHERE kind = 'incident';

CREATE UNIQUE INDEX notification_deliveries_test_unique
ON notification_deliveries (channel_id, test_request_id)
WHERE kind = 'test';

CREATE INDEX notification_deliveries_due
ON notification_deliveries (state, next_attempt_at, lease_until, created_at, id);

CREATE INDEX notification_deliveries_channel
ON notification_deliveries (channel_id, created_at DESC, id DESC);
