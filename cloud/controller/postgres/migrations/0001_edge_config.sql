CREATE TABLE edge_deployments (
    edge_id uuid PRIMARY KEY,
    name text NOT NULL,
    region text NOT NULL,
    capacity bigint NOT NULL CHECK (capacity > 0),
    public_endpoint text NOT NULL,
    enabled boolean NOT NULL,
    desired_config_version bigint NOT NULL CHECK (desired_config_version > 0),
    revision bigint NOT NULL CHECK (revision > 0),
    identity_csr_sha256 bytea,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE edge_config_versions (
    edge_id uuid NOT NULL REFERENCES edge_deployments(edge_id) ON DELETE CASCADE,
    version bigint NOT NULL CHECK (version > 0),
    key_id text NOT NULL,
    payload bytea NOT NULL,
    signature bytea NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (edge_id, version)
);

CREATE TABLE edge_claim_tokens (
    token_digest bytea PRIMARY KEY,
    edge_id uuid NOT NULL REFERENCES edge_deployments(edge_id) ON DELETE CASCADE,
    purpose text NOT NULL CHECK (purpose IN ('install', 'bootstrap')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL
);

CREATE INDEX edge_claim_tokens_edge_id_idx ON edge_claim_tokens(edge_id);
