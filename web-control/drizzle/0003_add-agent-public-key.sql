ALTER TABLE "agents" ADD COLUMN "public_key" text;--> statement-breakpoint
ALTER TABLE "agents" ADD COLUMN "claimed" boolean DEFAULT false NOT NULL;