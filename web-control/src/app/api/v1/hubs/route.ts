import { NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { hubs } from "@/lib/schema";
import { requireAccessToken } from "@/lib/termx-control";

export async function GET(request: Request) {
  const tokenRecord = await requireAccessToken(request);
  if (!tokenRecord) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const result = await db
    .select({
      id: hubs.id,
      region: hubs.region,
      httpUrl: hubs.httpUrl,
      status: hubs.status,
      agentCount: hubs.agentCount,
      maxAgents: hubs.maxAgents,
      lastHeartbeat: hubs.lastHeartbeat,
    })
    .from(hubs)
    .where(eq(hubs.status, "online"));

  return NextResponse.json({
    hubs: result.map((hub) => {
      const capacity = hub.maxAgents > 0 ? Math.max(0, hub.maxAgents - hub.agentCount) : 1000;
      return {
        id: hub.id,
        region: hub.region,
        http_url: hub.httpUrl,
        status: hub.status,
        capacity,
        weight: capacity,
        health: JSON.stringify({ ok: true }),
        expires_at: new Date(Date.now() + 60_000).toISOString(),
      };
    }),
  });
}
