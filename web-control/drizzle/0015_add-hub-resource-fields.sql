ALTER TABLE "hubs" ADD COLUMN "bandwidth_mbps" real DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE "hubs" ADD COLUMN "cpu_cores" real DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE "hubs" ADD COLUMN "memory_gb" real DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE "hubs" ADD COLUMN "max_agents" integer DEFAULT 0 NOT NULL;
