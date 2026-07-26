CREATE TABLE accounts (
    account_id uuid PRIMARY KEY,
    display_name text NOT NULL,
    state text NOT NULL CHECK (state IN ('active', 'disabled')),
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE daemons (
    daemon_id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    display_name text NOT NULL,
    device_id text NOT NULL UNIQUE,
    device_public_key bytea NOT NULL,
    device_fingerprint text NOT NULL UNIQUE,
    revoked boolean NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (octet_length(device_public_key) = 32)
);

CREATE INDEX daemons_account_id_idx ON daemons(account_id);

CREATE TABLE daemon_enrollment_tokens (
    token_digest bytea PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    daemon_name text NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL
);

CREATE INDEX daemon_enrollment_tokens_account_id_idx ON daemon_enrollment_tokens(account_id);
