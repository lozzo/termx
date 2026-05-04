import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { hubs, accessTokens } from "@/lib/schema";
import { eq, and, or, gt, isNull } from "drizzle-orm";
import crypto from "crypto";
import { cacheKeys, cacheManager, cacheTtl } from "@/lib/cache-system";

// GET /api/hubs/discover — 返回所有在线 Hub
// 认证方式: Authorization: Bearer <access_token>
export async function GET(request: Request) {
  const authHeader = request.headers.get("authorization");
  if (!authHeader?.startsWith("Bearer ")) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }
  const tokenPlain = authHeader.slice(7);
  const tokenHash = crypto.createHash("sha256").update(tokenPlain).digest("hex");

  const tokenRecord = await db.query.accessTokens.findFirst({
    where: and(
      eq(accessTokens.tokenHash, tokenHash),
      or(isNull(accessTokens.expiresAt), gt(accessTokens.expiresAt, new Date()))
    ),
  });

  if (!tokenRecord) {
    return NextResponse.json({ error: "invalid or expired token" }, { status: 401 });
  }

  const result = await cacheManager.getOrLoad({
    key: cacheKeys.hubsDiscover(),
    ttlMs: cacheTtl.hubsDiscover,
    loader: async () =>
      db
        .select({
          id: hubs.id,
          name: hubs.name,
          region: hubs.region,
          grpcUrl: hubs.grpcUrl,
          httpUrl: hubs.httpUrl,
          agentCount: hubs.agentCount,
          bandwidthMbps: hubs.bandwidthMbps,
          cpuCores: hubs.cpuCores,
          memoryGb: hubs.memoryGb,
          maxAgents: hubs.maxAgents,
        })
        .from(hubs)
        .where(eq(hubs.status, "online")),
  });

  return NextResponse.json({
    hubs: result.map((h) => ({
      id: h.id,
      name: h.name,
      region: h.region,
      grpc_url: h.grpcUrl,
      http_url: h.httpUrl,
      agent_count: h.agentCount,
      bandwidth_mbps: h.bandwidthMbps,
      cpu_cores: h.cpuCores,
      memory_gb: h.memoryGb,
      max_agents: h.maxAgents,
    })),
  });
}
