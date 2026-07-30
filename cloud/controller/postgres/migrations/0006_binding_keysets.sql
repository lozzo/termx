CREATE TABLE binding_keyset_history (
    purpose text NOT NULL,
    keyset_sha256 bytea NOT NULL CHECK (octet_length(keyset_sha256) = 32),
    revision bigint NOT NULL CHECK (revision > 0),
    first_seen_at timestamptz NOT NULL,
    PRIMARY KEY (purpose, keyset_sha256),
    UNIQUE (purpose, revision),
    UNIQUE (purpose, keyset_sha256, revision)
);

CREATE TABLE binding_keysets (
    purpose text PRIMARY KEY,
    keyset_sha256 bytea NOT NULL CHECK (octet_length(keyset_sha256) = 32),
    revision bigint NOT NULL CHECK (revision > 0),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > issued_at),
    CHECK (expires_at - issued_at <= interval '24 hours'),
    FOREIGN KEY (purpose, keyset_sha256, revision) REFERENCES binding_keyset_history(purpose, keyset_sha256, revision)
);
