ALTER TABLE daemons
    ADD COLUMN preferred_edge_id uuid REFERENCES edge_deployments(edge_id) ON DELETE SET NULL,
    ADD COLUMN edge_preference_revision bigint NOT NULL DEFAULT 1 CHECK (edge_preference_revision > 0),
    ADD COLUMN edge_preference_updated_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE daemon_edge_measurements (
    daemon_id uuid NOT NULL REFERENCES daemons(daemon_id) ON DELETE CASCADE,
    edge_id uuid NOT NULL REFERENCES edge_deployments(edge_id) ON DELETE CASCADE,
    reachable boolean NOT NULL,
    connect_latency_ms integer NOT NULL CHECK (connect_latency_ms >= 0),
    connection_failure_rate double precision NOT NULL CHECK (connection_failure_rate >= 0 AND connection_failure_rate <= 1),
    sample_count integer NOT NULL CHECK (sample_count > 0),
    measured_at timestamptz NOT NULL,
    PRIMARY KEY (daemon_id, edge_id)
);

CREATE INDEX daemon_edge_measurements_measured_at_idx ON daemon_edge_measurements(measured_at);
