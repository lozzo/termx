-- 优惠码表
CREATE TABLE IF NOT EXISTS "promo_codes" (
  "id" text PRIMARY KEY NOT NULL,
  "code" text NOT NULL UNIQUE,
  "discount_type" text NOT NULL,
  "discount_value" integer NOT NULL,
  "max_uses" integer,
  "used_count" integer NOT NULL DEFAULT 0,
  "starts_at" timestamp(3),
  "expires_at" timestamp(3),
  "active" boolean NOT NULL DEFAULT true,
  "created_at" timestamp(3) NOT NULL DEFAULT now()
);

-- 优惠码使用记录表
CREATE TABLE IF NOT EXISTS "promo_code_usages" (
  "id" text PRIMARY KEY NOT NULL,
  "promo_code_id" text NOT NULL REFERENCES "promo_codes"("id"),
  "user_id" text NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "order_id" text NOT NULL REFERENCES "orders"("id"),
  "discount_amount" integer NOT NULL,
  "created_at" timestamp(3) NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS "promo_code_usages_promo_code_id_idx" ON "promo_code_usages" ("promo_code_id");
CREATE INDEX IF NOT EXISTS "promo_code_usages_user_id_idx" ON "promo_code_usages" ("user_id");
ALTER TABLE "promo_code_usages" ADD CONSTRAINT "promo_code_usages_user_promo_unique" UNIQUE ("user_id", "promo_code_id");

-- orders 表新增优惠码相关字段
ALTER TABLE "orders" ADD COLUMN IF NOT EXISTS "promo_code_id" text;
ALTER TABLE "orders" ADD COLUMN IF NOT EXISTS "discount_amount" integer NOT NULL DEFAULT 0;
