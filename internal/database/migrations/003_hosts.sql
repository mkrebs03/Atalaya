CREATE TABLE hosts (
    id TEXT PRIMARY KEY NOT NULL,
    display_name TEXT NOT NULL UNIQUE,
    source TEXT NOT NULL CHECK(source IN ('local', 'enrolled')),
    agent_url TEXT NOT NULL UNIQUE,
    agent_credential BLOB NOT NULL,
    administrative_state TEXT NOT NULL CHECK(administrative_state IN ('pending', 'active', 'disabled')),
    observed_state TEXT NOT NULL CHECK(observed_state IN ('unknown', 'healthy', 'degraded', 'unreachable')),
    interval_seconds INTEGER NOT NULL CHECK(interval_seconds > 0),
    timeout_seconds INTEGER NOT NULL CHECK(timeout_seconds > 0 AND timeout_seconds < interval_seconds),
    last_attempt_at TEXT,
    last_success_received_at TEXT,
    freshness_deadline_at TEXT,
    consecutive_failures INTEGER NOT NULL CHECK(consecutive_failures >= 0),
    stale INTEGER NOT NULL CHECK(stale IN (0, 1)),
    archived_at TEXT,
    bootstrap_completed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    collection_generation INTEGER NOT NULL CHECK(collection_generation >= 1),
    version INTEGER NOT NULL CHECK(version > 0)
);

CREATE UNIQUE INDEX hosts_one_local ON hosts ((1)) WHERE source = 'local';
CREATE INDEX hosts_schedulable ON hosts (administrative_state, archived_at, bootstrap_completed_at);

CREATE TABLE host_snapshots (
    host_id TEXT PRIMARY KEY NOT NULL REFERENCES hosts(id),
    received_at TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    hostname TEXT NOT NULL,
    boot_id TEXT,
    uptime_seconds INTEGER CHECK(uptime_seconds >= 0),
    cpu_used_percent REAL CHECK(cpu_used_percent >= 0 AND cpu_used_percent <= 100),
    memory_used_bytes INTEGER CHECK(memory_used_bytes >= 0),
    memory_total_bytes INTEGER CHECK(memory_total_bytes >= 0),
    tailscale_state TEXT NOT NULL CHECK(tailscale_state IN ('connected', 'stopped', 'needs_login', 'unknown')),
    tailscale_hostname TEXT,
    agent_version TEXT NOT NULL,
    protocol_version INTEGER NOT NULL CHECK(protocol_version > 0),
    CHECK((memory_used_bytes IS NULL) = (memory_total_bytes IS NULL)),
    CHECK(memory_used_bytes IS NULL OR memory_used_bytes <= memory_total_bytes)
);

CREATE TABLE host_snapshot_warnings (
    host_id TEXT NOT NULL REFERENCES host_snapshots(host_id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    PRIMARY KEY (host_id, code)
);

CREATE TABLE host_snapshot_addresses (
    host_id TEXT NOT NULL REFERENCES host_snapshots(host_id) ON DELETE CASCADE,
    address TEXT NOT NULL,
    PRIMARY KEY (host_id, address)
);

CREATE TABLE filesystems (
    id TEXT PRIMARY KEY NOT NULL,
    host_id TEXT NOT NULL REFERENCES hosts(id),
    identity_kind TEXT NOT NULL CHECK(identity_kind IN ('uuid', 'fallback')),
    identity_key BLOB NOT NULL CHECK(length(identity_key) >= 13 AND substr(identity_key, 1, 1) = x'01'),
    fs_uuid TEXT,
    mount_root TEXT NOT NULL,
    mount_point TEXT NOT NULL,
    major_minor TEXT NOT NULL,
    fs_type TEXT NOT NULL,
    device TEXT NOT NULL,
    total_bytes INTEGER NOT NULL CHECK(total_bytes >= 0),
    used_bytes INTEGER NOT NULL CHECK(used_bytes >= 0),
    available_bytes INTEGER NOT NULL CHECK(available_bytes >= 0),
    discovered_at TEXT NOT NULL,
    selected INTEGER NOT NULL CHECK(selected IN (0, 1)),
    present INTEGER NOT NULL CHECK(present IN (0, 1)),
    last_observed_at TEXT NOT NULL,
    version INTEGER NOT NULL CHECK(version > 0),
    UNIQUE(host_id, identity_kind, identity_key),
    CHECK(fs_uuid IS NULL OR (identity_kind = 'uuid' AND fs_uuid = lower(fs_uuid)))
);

CREATE INDEX filesystems_host_mount ON filesystems (host_id, mount_point, id);

CREATE TABLE schedule_periods (
    id TEXT PRIMARY KEY NOT NULL,
    target_type TEXT NOT NULL CHECK(target_type IN ('host', 'check')),
    target_id TEXT NOT NULL,
    starts_at TEXT NOT NULL,
    ends_at TEXT,
    effective_interval_seconds INTEGER NOT NULL CHECK(effective_interval_seconds > 0),
    effective_timeout_seconds INTEGER NOT NULL CHECK(effective_timeout_seconds > 0 AND effective_timeout_seconds < effective_interval_seconds),
    CHECK(ends_at IS NULL OR ends_at > starts_at),
    UNIQUE(id, target_type, target_id)
);

CREATE UNIQUE INDEX schedule_periods_one_open ON schedule_periods (target_type, target_id) WHERE ends_at IS NULL;

CREATE TABLE schedule_slots (
    id TEXT PRIMARY KEY NOT NULL,
    target_type TEXT NOT NULL CHECK(target_type IN ('host', 'check')),
    target_id TEXT NOT NULL,
    schedule_period_id TEXT NOT NULL,
    nominal_at TEXT NOT NULL,
    not_before_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    effective_interval_seconds INTEGER NOT NULL CHECK(effective_interval_seconds > 0),
    effective_timeout_seconds INTEGER NOT NULL CHECK(effective_timeout_seconds > 0 AND effective_timeout_seconds < effective_interval_seconds),
    state TEXT NOT NULL CHECK(state IN ('scheduled', 'running', 'succeeded', 'failed', 'cancelled')),
    lease_until TEXT,
    process_attempts INTEGER NOT NULL CHECK(process_attempts >= 0),
    cancellation_reason TEXT CHECK(cancellation_reason IN ('missed_deadline', 'target_disabled', 'target_archived', 'target_changed')),
    started_at TEXT,
    finished_at TEXT,
    CHECK(not_before_at >= nominal_at AND deadline_at >= not_before_at),
    CHECK((state = 'running') = (lease_until IS NOT NULL)),
    CHECK((state = 'cancelled') = (cancellation_reason IS NOT NULL)),
    CHECK((state IN ('succeeded', 'failed', 'cancelled')) = (finished_at IS NOT NULL)),
    FOREIGN KEY(schedule_period_id, target_type, target_id) REFERENCES schedule_periods(id, target_type, target_id)
);

CREATE UNIQUE INDEX schedule_slots_one_nonterminal ON schedule_slots (target_type, target_id) WHERE state IN ('scheduled', 'running');
CREATE INDEX schedule_slots_due ON schedule_slots (state, not_before_at, deadline_at);

CREATE TRIGGER schedule_periods_host_target_insert
BEFORE INSERT ON schedule_periods WHEN NEW.target_type = 'host'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM hosts WHERE id = NEW.target_id AND bootstrap_completed_at IS NOT NULL
    ) THEN RAISE(ABORT, 'invalid host schedule target') END;
END;

CREATE TRIGGER schedule_periods_host_target_update
BEFORE UPDATE OF target_type, target_id ON schedule_periods WHEN NEW.target_type = 'host'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM hosts WHERE id = NEW.target_id AND bootstrap_completed_at IS NOT NULL
    ) THEN RAISE(ABORT, 'invalid host schedule target') END;
END;

CREATE TRIGGER schedule_slots_host_target_insert
BEFORE INSERT ON schedule_slots WHEN NEW.target_type = 'host'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM hosts WHERE id = NEW.target_id AND bootstrap_completed_at IS NOT NULL
    ) THEN RAISE(ABORT, 'invalid host schedule target') END;
END;

CREATE TRIGGER schedule_slots_host_target_update
BEFORE UPDATE OF target_type, target_id ON schedule_slots WHEN NEW.target_type = 'host'
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM hosts WHERE id = NEW.target_id AND bootstrap_completed_at IS NOT NULL
    ) THEN RAISE(ABORT, 'invalid host schedule target') END;
END;
