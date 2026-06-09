import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { hubs, agents } from "@/lib/schema";
import { getHubSecret } from "@/lib/config";
import { getUnsubscribedUserIds, getUserBandwidthLimits } from "@/lib/subscription";
import { eq, and, inArray, sql } from "drizzle-orm";
import { invalidateAdminAgents, invalidateUserAgents } from "@/lib/cache-system";

interface HeartbeatStaticInfo {
  name?: string;
  region?: string;
  http_url: string;
  grpc_url: string;
  bandwidth_mbps?: number;
  cpu_cores?: number;
  memory_gb?: number;
  max_agents?: number;
}

interface HeartbeatTrafficEntry {
  agent_id: string;
  bytes_in: number;
  bytes_out: number;
}

interface HeartbeatBody {
  hub_id: string;
  agent_ids: string[];
  traffic?: HeartbeatTrafficEntry[];
  static?: HeartbeatStaticInfo;
}

// POST /api/internal/hubs/heartbeat — 接收 Hub 心跳
export async function POST(request: Request) {
  const secret = request.headers.get("X-Hub-Secret") || request.headers.get("X-TermX-Hub-Secret");
  if (secret !== getHubSecret()) {
    return NextResponse.json({ error: "forbidden" }, { status: 403 });
  }

  const body: HeartbeatBody = await request.json();
  const { hub_id, agent_ids, traffic, static: staticInfo } = body;

  if (!hub_id) {
    return NextResponse.json({ error: "hub_id required" }, { status: 400 });
  }

  const agentIdList = agent_ids || [];

  const touchedUserIds = new Set<string>();

  // Upsert Hub 记录
  const hubValues: Record<string, unknown> = {
    id: hub_id,
    status: "online",
    agentCount: agentIdList.length,
    lastHeartbeat: new Date(),
  };
  const hubUpdateSet: Record<string, unknown> = {
    status: "online",
    agentCount: agentIdList.length,
    lastHeartbeat: new Date(),
  };

  // 有静态信息时更新静态字段
  if (staticInfo) {
    hubValues.name = staticInfo.name || hub_id;
    hubValues.region = staticInfo.region || "";
    hubValues.httpUrl = staticInfo.http_url || "";
    hubValues.grpcUrl = staticInfo.grpc_url || "";
    hubValues.bandwidthMbps = staticInfo.bandwidth_mbps || 0;
    hubValues.cpuCores = staticInfo.cpu_cores || 0;
    hubValues.memoryGb = staticInfo.memory_gb || 0;
    hubValues.maxAgents = staticInfo.max_agents || 0;
    hubUpdateSet.name = staticInfo.name || hub_id;
    hubUpdateSet.region = staticInfo.region || "";
    hubUpdateSet.httpUrl = staticInfo.http_url || "";
    hubUpdateSet.grpcUrl = staticInfo.grpc_url || "";
    hubUpdateSet.bandwidthMbps = staticInfo.bandwidth_mbps || 0;
    hubUpdateSet.cpuCores = staticInfo.cpu_cores || 0;
    hubUpdateSet.memoryGb = staticInfo.memory_gb || 0;
    hubUpdateSet.maxAgents = staticInfo.max_agents || 0;
  }

  await db
    .insert(hubs)
    .values(hubValues as typeof hubs.$inferInsert)
    .onConflictDoUpdate({
      target: hubs.id,
      set: hubUpdateSet,
    });

  // Agent 在线/离线同步（仅用 ID 列表做判断）
  if (agentIdList.length > 0) {
    // 将心跳列表中的 Agent 标记为在线
    await db
      .update(agents)
      .set({ online: true, hubId: hub_id, lastSeen: new Date() })
      .where(inArray(agents.id, agentIdList));
  }

  // 将不在心跳列表中但标记在该 hub 上在线的 Agent 设为离线
  // 增加 lastSeen 时间保护：仅将 lastSeen 超过 30 秒的 agent 设为离线
  // 避免与 agent report 即时上线之间的 race condition
  const staleThreshold = new Date(Date.now() - 30 * 1000);
  const staleThresholdMs = staleThreshold.getTime();
  if (agentIdList.length > 0) {
    // 该 hub 上 online 但不在 agent_ids 中的
    const currentOnline = await db
      .select({ id: agents.id, lastSeen: agents.lastSeen })
      .from(agents)
      .where(and(eq(agents.hubId, hub_id), eq(agents.online, true)));

    const reportedSet = new Set(agentIdList);
    const offlineIds = currentOnline
      .filter((a) => !reportedSet.has(a.id))
      .filter((a) => !a.lastSeen || a.lastSeen < staleThreshold)
      .map((a) => a.id);
    if (offlineIds.length > 0) {
      await db
        .update(agents)
        .set({ online: false, hubId: null })
        .where(inArray(agents.id, offlineIds));
    }
  } else {
    // 没有任何 agent，将该 hub 上所有 lastSeen 超时的在线 agent 设为离线
    await db
      .update(agents)
      .set({ online: false, hubId: null })
      .where(
        and(
          eq(agents.hubId, hub_id),
          eq(agents.online, true),
          sql`${agents.lastSeen} IS NULL OR ${agents.lastSeen} < ${staleThresholdMs}`
        )
      );
  }

  // 流量数据更新到 agents 表（累计值覆盖）
  if (traffic && traffic.length > 0) {
    await Promise.all(
      traffic.map((t) =>
        db
          .update(agents)
          .set({
            sessionBytesIn: t.bytes_in,
            sessionBytesOut: t.bytes_out,
          })
          .where(eq(agents.id, t.agent_id))
      )
    );
  }

  // 检查订阅状态
  let kickAgents: string[] = [];
  let blockedUsersList: string[] = [];
  let rateLimits: Record<string, number> = {};

  if (agentIdList.length > 0) {
    // 获取在线 Agent 的 userId
    const onlineAgents = await db
      .select({ id: agents.id, userId: agents.userId })
      .from(agents)
      .where(inArray(agents.id, agentIdList));

    for (const agent of onlineAgents) {
      if (agent.userId) touchedUserIds.add(agent.userId);
    }

    const userIds = [...new Set(onlineAgents.map((a) => a.userId).filter(Boolean))];

    if (userIds.length > 0) {
      const unsubscribedUsers = await getUnsubscribedUserIds(userIds);

      if (unsubscribedUsers.size > 0) {
        blockedUsersList = [...unsubscribedUsers];
        kickAgents = onlineAgents
          .filter((a) => unsubscribedUsers.has(a.userId))
          .map((a) => a.id);
      }
    }

    // 查询带宽限速
    const bandwidthLimits = await getUserBandwidthLimits(userIds);
    if (bandwidthLimits.size > 0) {
      for (const a of onlineAgents) {
        const kbps = bandwidthLimits.get(a.userId);
        if (kbps !== undefined) {
          rateLimits[a.id] = kbps;
        }
      }
    }
  }

  if (touchedUserIds.size > 0) {
    await Promise.all([
      ...Array.from(touchedUserIds, (userId) => invalidateUserAgents(userId)),
      invalidateAdminAgents(),
    ]);
  }

  return NextResponse.json({
    ok: true,
    kick_agents: kickAgents,
    blocked_users: blockedUsersList,
    rate_limits: rateLimits,
  });
}
