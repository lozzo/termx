ALTER TABLE "users" ADD COLUMN "github_id" text;
ALTER TABLE "users" ADD COLUMN "github_login" text;
ALTER TABLE "users" ADD COLUMN "github_avatar_url" text;
CREATE UNIQUE INDEX "users_github_id_unique" ON "users" USING btree ("github_id");
