ALTER TABLE accounts
    DROP CONSTRAINT accounts_state_check,
    ADD CONSTRAINT accounts_state_check CHECK (state IN ('pending', 'active', 'disabled'));

ALTER TABLE daemons
    ADD CONSTRAINT daemons_account_id_daemon_id_key UNIQUE (account_id, daemon_id);

CREATE TABLE account_credentials (
    account_id uuid PRIMARY KEY REFERENCES accounts(account_id) ON DELETE CASCADE,
    password_hash bytea,
    setup_digest bytea UNIQUE CHECK (setup_digest IS NULL OR octet_length(setup_digest) = 32),
    setup_expires_at timestamptz,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    CHECK (
        (password_hash IS NOT NULL AND setup_digest IS NULL AND setup_expires_at IS NULL) OR
        (password_hash IS NULL AND setup_digest IS NOT NULL AND setup_expires_at IS NOT NULL)
    )
);

INSERT INTO account_credentials(account_id, password_hash, revision, updated_at)
SELECT account_id, password_hash, revision, updated_at
FROM accounts;

ALTER TABLE accounts
    DROP COLUMN password_hash;

ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_subscription_id_account_id_key UNIQUE (subscription_id, account_id);

DROP TABLE relay_usage_aggregates;
DROP TABLE relay_usage_events;
DROP TABLE usage_periods;

CREATE TABLE usage_periods (
    account_id uuid NOT NULL,
    subscription_id uuid NOT NULL,
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    quota_bytes bigint NOT NULL CHECK (quota_bytes >= 0),
    committed_ingress_bytes bigint NOT NULL DEFAULT 0 CHECK (committed_ingress_bytes >= 0),
    committed_egress_bytes bigint NOT NULL DEFAULT 0 CHECK (committed_egress_bytes >= 0),
    recovery_bytes bigint NOT NULL DEFAULT 0 CHECK (recovery_bytes >= 0),
    held_bytes bigint NOT NULL DEFAULT 0 CHECK (held_bytes >= 0),
    policy_digest bytea NOT NULL CHECK (octet_length(policy_digest) = 32),
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, subscription_id, period_start, period_end),
    FOREIGN KEY (subscription_id, account_id) REFERENCES subscriptions(subscription_id, account_id),
    CHECK (period_end > period_start),
    CHECK (committed_ingress_bytes::numeric + committed_egress_bytes::numeric + recovery_bytes::numeric <= 9223372036854775807),
    CHECK (committed_ingress_bytes::numeric + committed_egress_bytes::numeric + recovery_bytes::numeric + held_bytes::numeric <= 9223372036854775807)
);

CREATE TABLE relay_reservations (
    reservation_id uuid PRIMARY KEY,
    request_digest bytea NOT NULL CHECK (octet_length(request_digest) = 32),
    account_id uuid NOT NULL,
    subscription_id uuid NOT NULL,
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    edge_id uuid NOT NULL REFERENCES edge_deployments(edge_id),
    daemon_id uuid NOT NULL,
    client_id text NOT NULL CHECK (client_id <> ''),
    session_id uuid NOT NULL,
    state text NOT NULL CHECK (state IN ('held', 'settled')),
    reserved_bytes bigint NOT NULL CHECK (reserved_bytes > 0),
    max_rate_bytes_per_second bigint NOT NULL CHECK (max_rate_bytes_per_second > 0),
    renew_sequence bigint NOT NULL CHECK (renew_sequence >= 0),
    authorized_until timestamptz NOT NULL,
    policy_digest bytea NOT NULL CHECK (octet_length(policy_digest) = 32),
    policy_snapshot bytea NOT NULL CHECK (octet_length(policy_snapshot) > 0),
    settlement_kind text CHECK (settlement_kind IN ('exact', 'recovery_max')),
    settled_ingress_bytes bigint CHECK (settled_ingress_bytes >= 0),
    settled_egress_bytes bigint CHECK (settled_egress_bytes >= 0),
    recovery_bytes bigint CHECK (recovery_bytes >= 0),
    observed_at timestamptz,
    settled_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (edge_id, session_id),
    FOREIGN KEY (account_id, subscription_id, period_start, period_end)
        REFERENCES usage_periods(account_id, subscription_id, period_start, period_end),
    FOREIGN KEY (account_id, daemon_id) REFERENCES daemons(account_id, daemon_id),
    CHECK (authorized_until <= period_end),
    CHECK (
        (state = 'held' AND settlement_kind IS NULL AND settled_ingress_bytes IS NULL AND
         settled_egress_bytes IS NULL AND recovery_bytes IS NULL AND observed_at IS NULL AND settled_at IS NULL)
        OR
        (state = 'settled' AND settlement_kind IS NOT NULL AND settled_ingress_bytes IS NOT NULL AND
         settled_egress_bytes IS NOT NULL AND recovery_bytes IS NOT NULL AND observed_at IS NOT NULL AND settled_at IS NOT NULL)
    ),
    CHECK (
        state <> 'settled' OR
        settled_ingress_bytes::numeric + settled_egress_bytes::numeric + recovery_bytes::numeric <= reserved_bytes::numeric
    ),
    CHECK (
        settlement_kind IS NULL OR
        (settlement_kind = 'exact' AND recovery_bytes = 0) OR
        (settlement_kind = 'recovery_max' AND settled_ingress_bytes = 0 AND settled_egress_bytes = 0 AND recovery_bytes = reserved_bytes)
    )
);

CREATE INDEX relay_reservations_usage_period_idx
    ON relay_reservations(account_id, subscription_id, period_start, period_end, state, authorized_until);
