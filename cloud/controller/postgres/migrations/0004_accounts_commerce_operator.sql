ALTER TABLE accounts
    ADD COLUMN email text;

CREATE UNIQUE INDEX accounts_email_normalized_idx
    ON accounts(lower(email))
    WHERE email IS NOT NULL;

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

CREATE TABLE account_roles (
    account_id uuid NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('user', 'operator', 'admin')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, role)
);

CREATE TABLE account_sessions (
    session_id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    access_token_digest bytea NOT NULL UNIQUE CHECK (octet_length(access_token_digest) = 32),
    refresh_token_digest bytea NOT NULL UNIQUE CHECK (octet_length(refresh_token_digest) = 32),
    csrf_token_digest bytea CHECK (csrf_token_digest IS NULL OR octet_length(csrf_token_digest) = 32),
    access_expires_at timestamptz NOT NULL,
    refresh_expires_at timestamptz NOT NULL,
    recent_auth_expires_at timestamptz,
    revoked_at timestamptz,
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL,
    CHECK (refresh_expires_at > access_expires_at)
);

CREATE INDEX account_sessions_account_id_idx ON account_sessions(account_id);

CREATE TABLE plan_catalog_versions (
    catalog_version bigint PRIMARY KEY CHECK (catalog_version > 0),
    state text NOT NULL CHECK (state IN ('draft', 'published', 'retired')),
    created_by uuid REFERENCES accounts(account_id),
    created_at timestamptz NOT NULL,
    published_at timestamptz
);

CREATE TABLE plans (
    plan_id text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    catalog_version bigint NOT NULL REFERENCES plan_catalog_versions(catalog_version),
    name text NOT NULL,
    description text NOT NULL,
    state text NOT NULL CHECK (state IN ('draft', 'published', 'retired')),
    billing_period_days integer NOT NULL CHECK (billing_period_days > 0),
    managed_p2p_enabled boolean NOT NULL,
    managed_p2p_max_concurrency integer NOT NULL CHECK (managed_p2p_max_concurrency >= 0),
    relay_enabled boolean NOT NULL,
    relay_max_concurrency integer NOT NULL CHECK (relay_max_concurrency >= 0),
    relay_max_bytes_per_period bigint NOT NULL CHECK (relay_max_bytes_per_period >= 0),
    relay_max_bytes_per_lease bigint NOT NULL CHECK (relay_max_bytes_per_lease >= 0),
    relay_max_rate_bytes_per_second bigint NOT NULL CHECK (relay_max_rate_bytes_per_second >= 0),
    cloud_daemon_limit integer NOT NULL CHECK (cloud_daemon_limit >= 0),
    allowed_regions text[] NOT NULL DEFAULT '{}',
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL,
    published_at timestamptz,
    PRIMARY KEY (plan_id, version)
);

CREATE TABLE plan_prices (
    plan_id text NOT NULL,
    plan_version bigint NOT NULL,
    billing_cycle text NOT NULL CHECK (billing_cycle IN ('monthly', 'yearly')),
    currency text NOT NULL CHECK (char_length(currency) = 3),
    minor_units bigint NOT NULL CHECK (minor_units >= 0),
    PRIMARY KEY (plan_id, plan_version, billing_cycle),
    FOREIGN KEY (plan_id, plan_version) REFERENCES plans(plan_id, version) ON DELETE CASCADE
);

CREATE TABLE orders (
    order_id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    plan_id text NOT NULL,
    plan_version bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'paid', 'payment_failed', 'refunded', 'revoked')),
    currency text NOT NULL CHECK (char_length(currency) = 3),
    amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
    provider text NOT NULL,
    provider_reference text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL,
    requested_transition text NOT NULL CHECK (requested_transition IN ('activate', 'renew', 'upgrade', 'downgrade')),
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL,
    settled_at timestamptz,
    UNIQUE (account_id, idempotency_key),
    FOREIGN KEY (plan_id, plan_version) REFERENCES plans(plan_id, version)
);

CREATE INDEX orders_account_created_idx ON orders(account_id, created_at DESC);

CREATE TABLE payment_attempts (
    payment_attempt_id uuid PRIMARY KEY,
    order_id uuid NOT NULL REFERENCES orders(order_id),
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    provider text NOT NULL,
    provider_reference text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX payment_attempts_order_idx ON payment_attempts(order_id, created_at);

CREATE TABLE payment_events (
    provider text NOT NULL,
    provider_event_id text NOT NULL,
    event_digest bytea NOT NULL CHECK (octet_length(event_digest) = 32),
    payment_attempt_id uuid REFERENCES payment_attempts(payment_attempt_id),
    order_id uuid NOT NULL REFERENCES orders(order_id),
    event_type text NOT NULL CHECK (event_type IN ('succeeded', 'failed', 'refunded', 'revoked', 'chargeback')),
    state text NOT NULL CHECK (state IN ('received', 'applied', 'rejected')),
    provider_reference text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL,
    applied_at timestamptz,
    PRIMARY KEY (provider, provider_event_id)
);

CREATE TABLE subscriptions (
    subscription_id uuid PRIMARY KEY,
    account_id uuid NOT NULL UNIQUE REFERENCES accounts(account_id),
    plan_id text NOT NULL,
    plan_version bigint NOT NULL,
    source_order_id uuid REFERENCES orders(order_id),
    state text NOT NULL CHECK (state IN ('active', 'cancel_at_period_end', 'canceled', 'suspended', 'expired', 'past_due')),
    cancel_at_period_end boolean NOT NULL,
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    UNIQUE (subscription_id, account_id),
    CHECK (period_end > period_start),
    FOREIGN KEY (plan_id, plan_version) REFERENCES plans(plan_id, version)
);

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

CREATE TABLE subscription_adjustments (
    adjustment_id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    subscription_id uuid NOT NULL REFERENCES subscriptions(subscription_id),
    transition text NOT NULL,
    reason text NOT NULL,
    actor_account_id uuid REFERENCES accounts(account_id),
    before_revision bigint NOT NULL,
    after_revision bigint NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE entitlement_overrides (
    override_id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    relay_max_bytes_per_period bigint,
    relay_max_concurrency integer,
    cloud_daemon_limit integer,
    reason text NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revision bigint NOT NULL CHECK (revision > 0),
    created_by uuid REFERENCES accounts(account_id),
    created_at timestamptz NOT NULL
);

CREATE INDEX entitlement_overrides_account_active_idx
    ON entitlement_overrides(account_id, expires_at DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE operator_audit_events (
    audit_id uuid PRIMARY KEY,
    actor_account_id uuid REFERENCES accounts(account_id),
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    reason text NOT NULL DEFAULT '',
    result text NOT NULL,
    correlation_id text NOT NULL,
    occurred_at timestamptz NOT NULL
);

CREATE INDEX operator_audit_occurred_idx ON operator_audit_events(occurred_at DESC, audit_id DESC);

INSERT INTO plan_catalog_versions(catalog_version, state, created_at, published_at)
VALUES (1, 'published', now(), now());

INSERT INTO plans(
    plan_id, version, catalog_version, name, description, state, billing_period_days,
    managed_p2p_enabled, managed_p2p_max_concurrency, relay_enabled, relay_max_concurrency,
    relay_max_bytes_per_period, relay_max_bytes_per_lease, relay_max_rate_bytes_per_second,
    cloud_daemon_limit, allowed_regions, revision, created_at, published_at
) VALUES
    ('starter', 1, 1, '基础版', '适合个人设备的完整 Cloud 连接能力。', 'published', 30,
     true, 2, true, 2, 5368709120, 1073741824, 10485760, 3, ARRAY['*'], 1, now(), now()),
    ('professional', 1, 1, '专业版', '适合多设备与高频远程工作的更高配额。', 'published', 30,
     true, 10, true, 8, 1099511627776, 10737418240, 52428800, 20, ARRAY['*'], 1, now(), now()),
    ('team', 1, 1, '团队版', '面向团队设备池和集中运营的共享能力。', 'published', 30,
     true, 50, true, 32, 5497558138880, 53687091200, 104857600, 100, ARRAY['*'], 1, now(), now());

INSERT INTO plan_prices(plan_id, plan_version, billing_cycle, currency, minor_units) VALUES
    ('starter', 1, 'monthly', 'CNY', 0),
    ('starter', 1, 'yearly', 'CNY', 0),
    ('professional', 1, 'monthly', 'CNY', 3900),
    ('professional', 1, 'yearly', 'CNY', 39900),
    ('team', 1, 'monthly', 'CNY', 12900),
    ('team', 1, 'yearly', 'CNY', 129900);

INSERT INTO account_roles(account_id, role, created_at)
SELECT account_id, 'user', now() FROM accounts;

INSERT INTO subscriptions(
    subscription_id, account_id, plan_id, plan_version, state, cancel_at_period_end,
    period_start, period_end, revision, updated_at
)
SELECT md5(account_id::text || ':starter')::uuid, account_id, 'starter', 1, 'active', false,
       date_trunc('month', now()), date_trunc('month', now()) + interval '1 month', 1, now()
FROM accounts;
