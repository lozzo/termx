ALTER TABLE "access_tokens" ALTER COLUMN "expires_at" DROP NOT NULL;
ALTER TABLE "agents" ADD COLUMN "encrypted_private_key" text;
ALTER TABLE "agents" ADD COLUMN "key_nonce" text;
DROP TABLE IF EXISTS "setup_tokens";
