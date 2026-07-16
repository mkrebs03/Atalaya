CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE instance_metadata (
    key TEXT PRIMARY KEY NOT NULL,
    value BLOB NOT NULL
);

CREATE TABLE administrators (
    id TEXT PRIMARY KEY NOT NULL,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    password_changed_at TEXT NOT NULL,
    disabled_at TEXT,
    version INTEGER NOT NULL
);

CREATE UNIQUE INDEX administrators_one_active
ON administrators ((1))
WHERE disabled_at IS NULL;
