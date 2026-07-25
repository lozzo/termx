CREATE TABLE promotions (
  promotion_id TEXT PRIMARY KEY,
  code TEXT NOT NULL UNIQUE,
  state INTEGER NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  effective_from BIGINT NOT NULL,
  effective_until BIGINT NOT NULL,
  projection BYTEA NOT NULL
);

CREATE TABLE promotion_redemptions (
  redemption_id TEXT PRIMARY KEY,
  promotion_id TEXT NOT NULL REFERENCES promotions(promotion_id),
  account_id TEXT NOT NULL,
  order_id TEXT NOT NULL UNIQUE,
  state INTEGER NOT NULL,
  expires_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  projection BYTEA NOT NULL
);
CREATE INDEX promotion_redemptions_capacity ON promotion_redemptions (promotion_id, state, account_id);
CREATE INDEX promotion_redemptions_expiry ON promotion_redemptions (state, expires_at);

CREATE TABLE subscription_adjustments (
  adjustment_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL,
  request_id TEXT NOT NULL UNIQUE,
  resulting_subscription_revision BIGINT NOT NULL CHECK (resulting_subscription_revision > 0),
  projection BYTEA NOT NULL
);
CREATE INDEX subscription_adjustments_account ON subscription_adjustments (account_id, resulting_subscription_revision);
