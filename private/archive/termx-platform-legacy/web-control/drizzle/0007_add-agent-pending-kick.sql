ALTER TABLE "agents" ADD COLUMN IF NOT EXISTS "pending_kick" boolean NOT NULL DEFAULT false;
