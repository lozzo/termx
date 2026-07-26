CREATE TABLE usage_periods (
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    relay_ingress_bytes bigint NOT NULL DEFAULT 0 CHECK (relay_ingress_bytes >= 0),
    relay_egress_bytes bigint NOT NULL DEFAULT 0 CHECK (relay_egress_bytes >= 0),
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, period_start),
    CHECK (period_end > period_start)
);

CREATE TABLE relay_usage_events (
    edge_id uuid NOT NULL REFERENCES edge_deployments(edge_id),
    event_id uuid NOT NULL,
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    lease_id uuid NOT NULL,
    daemon_id uuid NOT NULL REFERENCES daemons(daemon_id),
    client_id text NOT NULL,
    session_id uuid NOT NULL,
    allocation_id uuid NOT NULL,
    transport text NOT NULL CHECK (transport IN ('udp', 'tcp', 'tls')),
    ingress_bytes bigint NOT NULL CHECK (ingress_bytes >= 0),
    egress_bytes bigint NOT NULL CHECK (egress_bytes >= 0),
    started_at timestamptz NOT NULL,
    ended_at timestamptz NOT NULL,
    committed_at timestamptz NOT NULL,
    PRIMARY KEY (edge_id, event_id),
    CHECK (ended_at >= started_at)
);

CREATE INDEX relay_usage_events_account_period_idx ON relay_usage_events(account_id, ended_at);

CREATE TABLE relay_usage_aggregates (
    account_id uuid NOT NULL REFERENCES accounts(account_id),
    edge_id uuid NOT NULL REFERENCES edge_deployments(edge_id),
    period_start timestamptz NOT NULL,
    ingress_bytes bigint NOT NULL DEFAULT 0 CHECK (ingress_bytes >= 0),
    egress_bytes bigint NOT NULL DEFAULT 0 CHECK (egress_bytes >= 0),
    event_count bigint NOT NULL DEFAULT 0 CHECK (event_count >= 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, edge_id, period_start)
);
