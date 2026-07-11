CREATE TABLE "agent_releases" (
	"id" serial PRIMARY KEY NOT NULL,
	"version" text NOT NULL,
	"os" text DEFAULT 'linux' NOT NULL,
	"arch" text DEFAULT 'amd64' NOT NULL,
	"download_url" text NOT NULL,
	"sha256" text DEFAULT '' NOT NULL,
	"changelog" text DEFAULT '' NOT NULL,
	"force_update" boolean DEFAULT false NOT NULL,
	"active" boolean DEFAULT true NOT NULL,
	"created_at" timestamp (3) DEFAULT now() NOT NULL
);
--> statement-breakpoint
ALTER TABLE "access_tokens" ALTER COLUMN "token" DROP NOT NULL;--> statement-breakpoint
ALTER TABLE "password_reset_codes" ADD COLUMN "attempts" integer DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE "refresh_tokens" ADD COLUMN "token_lookup" text;--> statement-breakpoint
CREATE INDEX "agent_releases_os_arch_idx" ON "agent_releases" USING btree ("os","arch");--> statement-breakpoint
ALTER TABLE "refresh_tokens" ADD CONSTRAINT "refresh_tokens_token_lookup_unique" UNIQUE("token_lookup");