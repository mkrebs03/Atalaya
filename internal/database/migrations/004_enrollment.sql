CREATE TABLE enrollment_tokens (
    id TEXT PRIMARY KEY NOT NULL,
    token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
    label TEXT,
    created_by TEXT NOT NULL REFERENCES administrators(id),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    consumed_at TEXT,
    replay_until TEXT,
    request_id TEXT,
    payload_hash BLOB CHECK(payload_hash IS NULL OR length(payload_hash) = 32),
    host_id TEXT REFERENCES hosts(id),
    version INTEGER NOT NULL CHECK(version > 0),
    CHECK((consumed_at IS NULL AND replay_until IS NULL AND request_id IS NULL AND payload_hash IS NULL AND host_id IS NULL)
        OR (consumed_at IS NOT NULL AND replay_until IS NOT NULL AND request_id IS NOT NULL AND payload_hash IS NOT NULL AND host_id IS NOT NULL))
);

CREATE INDEX enrollment_tokens_expiry ON enrollment_tokens (expires_at, revoked_at, consumed_at);
CREATE INDEX enrollment_tokens_host ON enrollment_tokens (host_id);
