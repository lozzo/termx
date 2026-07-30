CREATE TABLE binding_keysets (
    purpose text PRIMARY KEY,
    keyset_sha256 bytea NOT NULL CHECK (octet_length(keyset_sha256) = 32),
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL
);
