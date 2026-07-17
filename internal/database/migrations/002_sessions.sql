CREATE TABLE sessions (
    id TEXT PRIMARY KEY NOT NULL,
    administrator_id TEXT NOT NULL REFERENCES administrators(id),
    token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
    created_at TEXT NOT NULL,
    last_activity_at TEXT NOT NULL,
    idle_expires_at TEXT NOT NULL,
    absolute_expires_at TEXT NOT NULL,
    revoked_at TEXT,
    remote_addr TEXT NOT NULL,
    user_agent TEXT NOT NULL CHECK(length(CAST(user_agent AS BLOB)) <= 512),
    tailscale_login TEXT,
    tailscale_name TEXT,
    proxy_identity_informational INTEGER NOT NULL CHECK(proxy_identity_informational IN (0, 1))
);

CREATE INDEX sessions_administrator_active
ON sessions (administrator_id, revoked_at, absolute_expires_at);
