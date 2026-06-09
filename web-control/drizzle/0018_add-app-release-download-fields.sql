ALTER TABLE "app_releases" ADD COLUMN "download_url" text NOT NULL DEFAULT '';
ALTER TABLE "app_releases" ADD COLUMN "mirrors" text NOT NULL DEFAULT '[]';
