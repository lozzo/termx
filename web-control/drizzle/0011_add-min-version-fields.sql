ALTER TABLE "agent_releases" ADD COLUMN "min_app_version" text NOT NULL DEFAULT '';
ALTER TABLE "app_releases" ADD COLUMN "min_agent_version" text NOT NULL DEFAULT '';
