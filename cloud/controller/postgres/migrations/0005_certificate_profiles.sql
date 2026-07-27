CREATE TABLE certificate_profiles (
    certificate_profile_id uuid PRIMARY KEY,
    name text NOT NULL,
    dns_names text[] NOT NULL,
    sha256_fingerprint text NOT NULL,
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    secret_ref text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE edge_certificate_bindings (
    edge_id uuid PRIMARY KEY REFERENCES edge_deployments(edge_id) ON DELETE CASCADE,
    certificate_profile_id uuid NOT NULL REFERENCES certificate_profiles(certificate_profile_id),
    binding_revision bigint NOT NULL CHECK (binding_revision > 0),
    desired_revision bigint NOT NULL CHECK (desired_revision > 0),
    applied_profile_id uuid,
    applied_revision bigint NOT NULL DEFAULT 0 CHECK (applied_revision >= 0),
    last_error_code text NOT NULL DEFAULT '',
    last_error_message text NOT NULL DEFAULT '',
    applied_at timestamptz,
    updated_at timestamptz NOT NULL
);

CREATE INDEX edge_certificate_bindings_profile_idx ON edge_certificate_bindings(certificate_profile_id, edge_id);
