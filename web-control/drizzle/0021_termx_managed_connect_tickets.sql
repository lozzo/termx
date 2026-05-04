CREATE TABLE "managed_connect_tickets" (
  "id" text PRIMARY KEY NOT NULL,
  "user_id" text NOT NULL,
  "machine_id" text NOT NULL,
  "terminal_id" text NOT NULL,
  "path" text DEFAULT 'managed' NOT NULL,
  "allow_relay" boolean DEFAULT false NOT NULL,
  "relay_in_use" boolean DEFAULT false NOT NULL,
  "relay_bytes_remaining" bigint,
  "relay_throttled" boolean DEFAULT false NOT NULL,
  "expires_at" timestamp(3) NOT NULL,
  "consumed_at" timestamp(3),
  "created_at" timestamp(3) DEFAULT now() NOT NULL
);

ALTER TABLE "managed_connect_tickets"
  ADD CONSTRAINT "managed_connect_tickets_user_id_users_id_fk"
  FOREIGN KEY ("user_id") REFERENCES "public"."users"("id")
  ON DELETE cascade ON UPDATE no action;

ALTER TABLE "managed_connect_tickets"
  ADD CONSTRAINT "managed_connect_tickets_machine_id_agents_id_fk"
  FOREIGN KEY ("machine_id") REFERENCES "public"."agents"("id")
  ON DELETE cascade ON UPDATE no action;

CREATE INDEX "managed_connect_tickets_user_id_idx" ON "managed_connect_tickets" USING btree ("user_id");
CREATE INDEX "managed_connect_tickets_machine_id_idx" ON "managed_connect_tickets" USING btree ("machine_id");
CREATE INDEX "managed_connect_tickets_expires_at_idx" ON "managed_connect_tickets" USING btree ("expires_at");
