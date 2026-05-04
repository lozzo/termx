-- 用户表新增邀请码相关字段
ALTER TABLE "users" ADD COLUMN "referral_code" text;
ALTER TABLE "users" ADD COLUMN "referred_by" text;
ALTER TABLE "users" ADD CONSTRAINT "users_referral_code_unique" UNIQUE("referral_code");

-- 为已有用户生成邀请码
UPDATE "users" SET "referral_code" = substr(md5(random()::text || id), 1, 8) WHERE "referral_code" IS NULL;

-- 邀请推荐表
CREATE TABLE IF NOT EXISTS "referrals" (
  "id" text PRIMARY KEY NOT NULL,
  "referrer_id" text NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "invitee_id" text NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "status" text NOT NULL DEFAULT 'pending',
  "rewarded" boolean NOT NULL DEFAULT false,
  "reward_days" integer NOT NULL DEFAULT 3,
  "created_at" timestamp(3) NOT NULL DEFAULT now(),
  "rewarded_at" timestamp(3)
);

CREATE INDEX IF NOT EXISTS "referrals_referrer_id_idx" ON "referrals" ("referrer_id");
CREATE INDEX IF NOT EXISTS "referrals_invitee_id_idx" ON "referrals" ("invitee_id");
ALTER TABLE "referrals" ADD CONSTRAINT "referrals_referrer_invitee_unique" UNIQUE("referrer_id", "invitee_id");
