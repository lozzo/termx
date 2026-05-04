import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents, relayTraffic } from "@/lib/schema";
import { getHubSecret } from "@/lib/config";
import { inArray, eq } from "drizzle-orm";
import { invalidateAdminAgents, invalidateUserAgents } from "@/lib/cache-system";

interface AgentReportEntry {
  agent_id: string;
  user_id: string;
  token_id: string;
  hostname: string;
  os_info: string;
}

interface AgentOfflineEntry {
  agent_id: string;
  bytes_in: number;
  bytes_out: number;
}

interface ClosedSessionEntry {
  session_id: string;
  agent_id: string;
  user_id: string;
  bytes_in: number;
  bytes_out: number;
  connected_at: number; // unix timestamp
  duration: number;     // 秒（Go 端计算）
}

interface AgentReportBody {
  hub_id: string;
  online?: AgentReportEntry[];
  offline?: AgentOfflineEntry[];
  sessions?: ClosedSessionEntry[];
}

// POST /api/internal/hubs/agents/report — Agent 即时上下线上报
export async function POST(request: Request) {
  const secret = request.headers.get("X-Hub-Secret") || request.headers.get("X-TermX-Hub-Secret");
  if (secret !== getHubSecret()) {
    return NextResponse.json({ error: "forbidden" }, { status: 403 });
  }

  const body: AgentReportBody = await request.json();
  const { hub_id, online, offline, sessions } = body;

  if (!hub_id) {
    return NextResponse.json({ error: "hub_id required" }, { status: 400 });
  }

  const touchedUserIds = new Set<string>();

  // 处理上线的 Agent
  if (online && online.length > 0) {
    const now = new Date();
    for (const agent of online) {
      const [existing] = await db
        .select({ id: agents.id, tokenId: agents.tokenId })
        .from(agents)
        .where(eq(agents.id, agent.agent_id))
        .limit(1);

      const values = {
        userId: agent.user_id,
        tokenId: agent.token_id || existing?.tokenId || null,
        hostname: agent.hostname || "",
        osInfo: agent.os_info || "",
        online: true,
        hubId: hub_id,
        sessionStartedAt: now,
        sessionBytesIn: null,
        sessionBytesOut: null,
        lastSeen: now,
      };

      if (existing) {
        await db.update(agents).set(values).where(eq(agents.id, agent.agent_id));
      } else {
        await db.insert(agents).values({ id: agent.agent_id, ...values });
      }
    }

    for (const a of online) {
      if (a.user_id) touchedUserIds.add(a.user_id);
    }
  }

  // 处理关闭的客户端 session → INSERT relay_traffic
  if (sessions && sessions.length > 0) {
    const sessionValues = sessions.map((s) => {
      const connectedAt = new Date(s.connected_at * 1000);
      const disconnectedAt = new Date(s.connected_at * 1000 + s.duration * 1000);
      return {
        sessionId: s.session_id,
        agentId: s.agent_id,
        userId: s.user_id,
        hubId: hub_id,
        bytesIn: s.bytes_in,
        bytesOut: s.bytes_out,
        sessionType: "ws_events" as const,
        connectedAt,
        disconnectedAt,
        duration: s.duration,
      };
    });
    await db.insert(relayTraffic).values(sessionValues);
  }

  // 处理下线的 Agent
  if (offline && offline.length > 0) {
    const offlineIds = offline.map((a) => a.agent_id);

    // 写 agent_offline 汇总记录（agent 级 HTTP/gRPC/TURN 流量）
    const offlineWithTraffic = offline.filter((a) => a.bytes_in > 0 || a.bytes_out > 0);
    if (offlineWithTraffic.length > 0) {
      // 查询 agent 对应的 userId
      const agentUsers = await db
        .select({ id: agents.id, userId: agents.userId, sessionStartedAt: agents.sessionStartedAt })
        .from(agents)
        .where(inArray(agents.id, offlineWithTraffic.map((a) => a.agent_id)));

      const agentMap = new Map(agentUsers.map((a) => [a.id, a]));
      const now = new Date();

      for (const info of agentUsers) {
        if (info.userId) touchedUserIds.add(info.userId);
      }

      const offlineRecords = offlineWithTraffic
        .filter((a) => agentMap.has(a.agent_id))
        .map((a) => {
          const info = agentMap.get(a.agent_id)!;
          const connectedAt = info.sessionStartedAt || now;
          const duration = Math.round((now.getTime() - connectedAt.getTime()) / 1000);
          return {
            agentId: a.agent_id,
            userId: info.userId,
            hubId: hub_id,
            bytesIn: a.bytes_in,
            bytesOut: a.bytes_out,
            sessionType: "agent_offline" as const,
            connectedAt,
            disconnectedAt: now,
            duration,
          };
        });

      if (offlineRecords.length > 0) {
        await db.insert(relayTraffic).values(offlineRecords);
      }
    }

    // 重置 agents 表字段
    await db
      .update(agents)
      .set({
        online: false,
        hubId: null,
        sessionBytesIn: null,
        sessionBytesOut: null,
        sessionStartedAt: null,
      })
      .where(inArray(agents.id, offlineIds));
  }

  if (touchedUserIds.size > 0) {
    await Promise.all([
      ...Array.from(touchedUserIds, (userId) => invalidateUserAgents(userId)),
      invalidateAdminAgents(),
    ]);
  }

  return NextResponse.json({ ok: true });
}
