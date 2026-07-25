ALTER TABLE commerce_payment_attempts ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE commerce_payment_attempts ADD COLUMN status INTEGER NOT NULL DEFAULT 0;
ALTER TABLE commerce_payment_attempts ADD COLUMN provider_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE commerce_payment_attempts ADD COLUMN provider_transaction_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE commerce_payment_attempts ADD COLUMN provider_subscription_reference TEXT NOT NULL DEFAULT '';
ALTER TABLE commerce_payment_attempts ADD COLUMN reconcile_after BIGINT NOT NULL DEFAULT 0;
ALTER TABLE commerce_payment_attempts ADD COLUMN reconcile_deadline BIGINT NOT NULL DEFAULT 0;
ALTER TABLE commerce_payment_attempts ADD COLUMN provider_created_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE commerce_payment_attempts ADD COLUMN provider_updated_at BIGINT NOT NULL DEFAULT 0;

CREATE INDEX commerce_payment_attempts_reconcile ON commerce_payment_attempts (provider, status, reconcile_after);
CREATE UNIQUE INDEX commerce_payment_attempts_provider_checkout ON commerce_payment_attempts (provider, provider_reference) WHERE provider_reference <> '';
CREATE UNIQUE INDEX commerce_payment_attempts_provider_transaction ON commerce_payment_attempts (provider, provider_transaction_reference) WHERE provider_transaction_reference <> '';
CREATE INDEX commerce_payment_attempts_provider_subscription ON commerce_payment_attempts (provider, provider_subscription_reference) WHERE provider_subscription_reference <> '';
