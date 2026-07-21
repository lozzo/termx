CREATE TABLE hub_deployments (
  hub_id TEXT PRIMARY KEY,
  deployment_id TEXT NOT NULL,
  credential_fingerprint TEXT NOT NULL,
  control_public_key BYTEA NOT NULL,
  relay_control_public_key BYTEA NOT NULL,
  region TEXT NOT NULL,
  public_label TEXT NOT NULL,
  relay_id TEXT NOT NULL UNIQUE,
  relay_credential_fingerprint TEXT NOT NULL,
  enabled INTEGER NOT NULL,
  last_control_generation BIGINT NOT NULL CHECK (last_control_generation >= 0),
  last_relay_control_generation BIGINT NOT NULL CHECK (last_relay_control_generation >= 0),
  updated_at TEXT NOT NULL
);

CREATE TABLE hub_assignments (
  daemon_device_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  hub_id TEXT NOT NULL,
  assignment_epoch BIGINT NOT NULL CHECK (assignment_epoch > 0),
  not_before_unix_millis BIGINT NOT NULL,
  expires_at_unix_millis BIGINT NOT NULL,
  fence_satisfied INTEGER NOT NULL,
  previous_hub_id TEXT NOT NULL,
  previous_epoch BIGINT NOT NULL CHECK (previous_epoch >= 0),
  updated_at TEXT NOT NULL
);

CREATE TABLE hub_projection_heads (
  hub_id TEXT PRIMARY KEY,
  projection_revision BIGINT NOT NULL CHECK (projection_revision >= 0),
  digest BYTEA NOT NULL,
  published_at TEXT NOT NULL,
  acknowledged_at TEXT
);

CREATE TABLE control_receive_cursors (
  hub_id TEXT NOT NULL,
  control_generation BIGINT NOT NULL CHECK (control_generation > 0),
  sender_role INTEGER NOT NULL,
  accepted_sequence BIGINT NOT NULL CHECK (accepted_sequence >= 0),
  accepted_digest BYTEA NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (hub_id, control_generation, sender_role)
);

CREATE TABLE cloud_device_ownership (
  device_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  device_kind INTEGER NOT NULL,
  auth_epoch BIGINT NOT NULL CHECK (auth_epoch > 0),
  revoked INTEGER NOT NULL,
  public_key BYTEA NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE hub_topology_heads (
  hub_id TEXT PRIMARY KEY,
  control_generation BIGINT NOT NULL CHECK (control_generation > 0),
  topology_revision BIGINT NOT NULL CHECK (topology_revision > 0),
  topology_digest BYTEA NOT NULL,
  observed_at TEXT NOT NULL
);

CREATE TABLE presence_topology (
  daemon_device_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  hub_id TEXT NOT NULL,
  control_generation BIGINT NOT NULL CHECK (control_generation > 0),
  projection BYTEA NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE managed_peer_topology (
  daemon_device_id TEXT NOT NULL,
  managed_session_id TEXT NOT NULL,
  session_incarnation BIGINT NOT NULL CHECK (session_incarnation > 0),
  account_id TEXT NOT NULL,
  hub_id TEXT NOT NULL,
  control_generation BIGINT NOT NULL CHECK (control_generation > 0),
  projection BYTEA NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (daemon_device_id, managed_session_id, session_incarnation)
);

CREATE TABLE terminal_access_topology (
  daemon_device_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  hub_id TEXT NOT NULL,
  control_generation BIGINT NOT NULL CHECK (control_generation > 0),
  access_projection_revision BIGINT NOT NULL CHECK (access_projection_revision > 0),
  freshness INTEGER NOT NULL,
  inventory BYTEA NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE commerce_accounts (
  account_id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  projection BYTEA NOT NULL,
  password_hash BYTEA NOT NULL,
  auth_revision BIGINT NOT NULL CHECK (auth_revision > 0)
);

CREATE TABLE commerce_sessions (
  session_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  access_hash BYTEA NOT NULL UNIQUE,
  refresh_hash BYTEA NOT NULL UNIQUE,
  access_expires_at BIGINT NOT NULL,
  refresh_expires_at BIGINT NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  revoked INTEGER NOT NULL,
  client_device_id TEXT NOT NULL
);
CREATE INDEX commerce_sessions_account ON commerce_sessions (account_id);

CREATE TABLE commerce_orders (
  order_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  projection BYTEA NOT NULL
);
CREATE INDEX commerce_orders_account ON commerce_orders (account_id);

CREATE TABLE commerce_payment_attempts (
  payment_attempt_id TEXT PRIMARY KEY,
  order_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  projection BYTEA NOT NULL
);
CREATE INDEX commerce_payment_attempts_account ON commerce_payment_attempts (account_id);

CREATE TABLE commerce_payment_events (
  provider_event_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  digest BYTEA NOT NULL,
  event BYTEA NOT NULL,
  state INTEGER NOT NULL,
  result BYTEA
);

CREATE TABLE commerce_subscriptions (
  account_id TEXT PRIMARY KEY,
  revision BIGINT NOT NULL CHECK (revision > 0),
  projection BYTEA NOT NULL
);

CREATE TABLE commerce_entitlements (
  account_id TEXT PRIMARY KEY,
  projection BYTEA NOT NULL
);

CREATE TABLE relay_quota_periods (
  account_id TEXT NOT NULL,
  period_start_unix_millis BIGINT NOT NULL,
  period_end_unix_millis BIGINT NOT NULL,
  limit_bytes BIGINT NOT NULL CHECK (limit_bytes >= 0),
  used_bytes BIGINT NOT NULL CHECK (used_bytes >= 0),
  revision BIGINT NOT NULL CHECK (revision > 0),
  PRIMARY KEY (account_id, period_start_unix_millis)
);

CREATE TABLE relay_lease_reservations (
  lease_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  managed_session_id TEXT NOT NULL,
  client_device_id TEXT NOT NULL,
  target_device_id TEXT NOT NULL,
  region TEXT NOT NULL,
  period_start_unix_millis BIGINT NOT NULL,
  period_end_unix_millis BIGINT NOT NULL,
  reserved_bytes BIGINT NOT NULL CHECK (reserved_bytes >= 0),
  used_bytes BIGINT NOT NULL CHECK (used_bytes >= 0),
  state INTEGER NOT NULL,
  expires_at_unix_millis BIGINT NOT NULL,
  release_after_unix_millis BIGINT NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  projection BYTEA NOT NULL
);
CREATE INDEX relay_reservations_account_period ON relay_lease_reservations (account_id, period_start_unix_millis, state);

CREATE TABLE relay_usage_events (
  relay_id TEXT NOT NULL,
  lease_id TEXT NOT NULL,
  sequence BIGINT NOT NULL CHECK (sequence > 0),
  event_id TEXT NOT NULL UNIQUE,
  digest BYTEA NOT NULL,
  record BYTEA NOT NULL,
  created_at_unix_millis BIGINT NOT NULL,
  PRIMARY KEY (relay_id, lease_id, sequence)
);

CREATE TABLE relay_usage_aggregates (
  account_id TEXT NOT NULL,
  managed_session_id TEXT NOT NULL,
  route_id TEXT NOT NULL,
  period_start_unix_millis BIGINT NOT NULL,
  projection BYTEA NOT NULL,
  PRIMARY KEY (account_id, managed_session_id, route_id, period_start_unix_millis)
);

CREATE TABLE commerce_audit (
  audit_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  occurred_at BIGINT NOT NULL,
  projection BYTEA NOT NULL
);

CREATE TABLE management_commands (
  command_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  command_kind INTEGER NOT NULL,
  delivery_state INTEGER NOT NULL,
  execution_state INTEGER NOT NULL,
  expires_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  version BIGINT NOT NULL CHECK (version > 0),
  projection BYTEA NOT NULL,
  UNIQUE (account_id, idempotency_key)
);
CREATE INDEX management_commands_open ON management_commands (execution_state, updated_at);

CREATE TABLE management_command_children (
  child_command_id TEXT PRIMARY KEY,
  parent_command_id TEXT NOT NULL REFERENCES management_commands(command_id) ON DELETE CASCADE,
  target_hub_id TEXT NOT NULL
);

CREATE TABLE management_command_results (
  child_command_id TEXT NOT NULL REFERENCES management_command_children(child_command_id) ON DELETE CASCADE,
  result_kind TEXT NOT NULL,
  digest BYTEA NOT NULL,
  result BYTEA NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (child_command_id, result_kind)
);
