-- agents 表新增会话级流量字段
ALTER TABLE "agents" ADD COLUMN "session_bytes_in" bigint;
ALTER TABLE "agents" ADD COLUMN "session_bytes_out" bigint;
ALTER TABLE "agents" ADD COLUMN "session_started_at" timestamp(3);

-- relay_traffic 表改为会话模型
ALTER TABLE "relay_traffic" ADD COLUMN "session_id" text;
ALTER TABLE "relay_traffic" ADD COLUMN "session_type" text;
ALTER TABLE "relay_traffic" ADD COLUMN "connected_at" timestamp(3);
ALTER TABLE "relay_traffic" ADD COLUMN "disconnected_at" timestamp(3);
ALTER TABLE "relay_traffic" ADD COLUMN "duration" integer;

-- 删除旧的周期字段
ALTER TABLE "relay_traffic" DROP COLUMN IF EXISTS "period_start";
ALTER TABLE "relay_traffic" DROP COLUMN IF EXISTS "period_end";

-- 删除旧索引，创建新索引
DROP INDEX IF EXISTS "relay_traffic_period_idx";
CREATE INDEX IF NOT EXISTS "relay_traffic_session_id_idx" ON "relay_traffic" ("session_id");
