import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents, subscriptions, plans } from "@/lib/schema";
import { getHubSecret } from "@/lib/config";
import { getUserOverrides } from "@/lib/subscription";
import { eq, and } from "drizzle-orm";

// POST /api/internal/agents/verify — Hub 验证 Agent 公钥
export async function POST(request: NextRequest) {
  // 验证 Hub Secret
  const hubSecret = getHubSecret();
  const provided = request.headers.get("x-hub-secret") || request.headers.get("x-termx-hub-secret");
  if (!provided || provided !== hubSecret) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const body = await request.json();
  const { agent_id, public_key } = body;

  if (!agent_id || !public_key) {
    return NextResponse.json(
      { error: "agent_id and public_key are required" },
      { status: 400 }
    );
  }

  // 查询 Agent，检查 publicKey 匹配且有 userId
  const agent = await db.query.agents.findFirst({
    where: and(
      eq(agents.id, agent_id),
      eq(agents.publicKey, public_key),
    ),
  });

  if (!agent || !agent.userId) {
    return NextResponse.json({
      valid: false,
      user_id: "",
    });
  }

  // 查询用户订阅套餐的限速配置
  let relayBandwidthKbps = 0;
  let allowRelayTransfer = agent.allowRelayTransfer;

  const sub = await db.query.subscriptions.findFirst({
    where: eq(subscriptions.userId, agent.userId),
    with: { plan: true },
  });
  if (sub?.plan) {
    relayBandwidthKbps = (sub.plan as typeof plans.$inferSelect).relayBandwidthKbps ?? 0;
  }

  // 查询 userOverrides，override 值优先
  const overrides = await getUserOverrides(agent.userId);
  if (overrides) {
    if (overrides.relayBandwidthKbps != null) {
      relayBandwidthKbps = overrides.relayBandwidthKbps;
    }
    if (overrides.allowRelayTransfer != null) {
      allowRelayTransfer = overrides.allowRelayTransfer;
    }
  }

  return NextResponse.json({
    valid: true,
    user_id: agent.userId,
    token_id: agent.tokenId || "",
    allow_relay_transfer: allowRelayTransfer,
    relay_bandwidth_kbps: relayBandwidthKbps,
  });
}
