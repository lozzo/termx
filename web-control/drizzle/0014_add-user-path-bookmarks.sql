CREATE TABLE IF NOT EXISTS "user_path_bookmarks" (
  "id" text PRIMARY KEY NOT NULL,
  "user_id" text NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "agent_id" text NOT NULL REFERENCES "agents"("id") ON DELETE CASCADE,
  "path" text NOT NULL,
  "name" text NOT NULL,
  "created_at" timestamp(3) DEFAULT now() NOT NULL
);

CREATE INDEX IF NOT EXISTS "user_path_bookmarks_user_id_idx" ON "user_path_bookmarks" ("user_id");
CREATE INDEX IF NOT EXISTS "user_path_bookmarks_agent_id_idx" ON "user_path_bookmarks" ("agent_id");
