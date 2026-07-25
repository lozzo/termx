ALTER TABLE hub_deployments
  ADD COLUMN public_hub_url TEXT NOT NULL DEFAULT '',
  ADD COLUMN health_url TEXT NOT NULL DEFAULT '',
  ADD COLUMN max_assignments BIGINT NOT NULL DEFAULT 0 CHECK (max_assignments >= 0),
  ADD COLUMN identity_approved INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN draining INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN archived INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN directory_revision BIGINT NOT NULL DEFAULT 1 CHECK (directory_revision > 0);
