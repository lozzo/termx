CREATE TABLE plan_catalog_releases (
  catalog_version BIGINT PRIMARY KEY CHECK (catalog_version > 0),
  request_id TEXT NOT NULL UNIQUE,
  published_at BIGINT NOT NULL,
  projection BYTEA NOT NULL
);

CREATE TABLE plan_catalog_head (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  catalog_version BIGINT NOT NULL REFERENCES plan_catalog_releases(catalog_version),
  revision BIGINT NOT NULL CHECK (revision > 0)
);

CREATE TABLE plan_definitions (
  plan_id TEXT NOT NULL,
  plan_version BIGINT NOT NULL CHECK (plan_version > 0),
  projection BYTEA NOT NULL,
  PRIMARY KEY (plan_id, plan_version)
);

CREATE TABLE plan_catalog_release_plans (
  catalog_version BIGINT NOT NULL REFERENCES plan_catalog_releases(catalog_version),
  plan_id TEXT NOT NULL,
  plan_version BIGINT NOT NULL,
  PRIMARY KEY (catalog_version, plan_id),
  FOREIGN KEY (plan_id, plan_version) REFERENCES plan_definitions(plan_id, plan_version)
);

CREATE TABLE entitlement_overrides (
  override_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  effective_from BIGINT NOT NULL,
  effective_until BIGINT NOT NULL,
  revoked_at BIGINT NOT NULL,
  activation_applied INTEGER NOT NULL,
  expiration_applied INTEGER NOT NULL,
  projection BYTEA NOT NULL
);
CREATE INDEX entitlement_overrides_account_window ON entitlement_overrides (account_id, effective_from, effective_until, revoked_at);

CREATE TABLE operator_mutation_audit (
  audit_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  occurred_at BIGINT NOT NULL,
  projection BYTEA NOT NULL
);
