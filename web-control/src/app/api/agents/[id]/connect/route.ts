import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents } from "@/lib/schema";
import { getAuthFromRequest, signHubUserToken } from "@/lib/auth";
import { getActiveSubscription, getUserOverrides } from "@/lib/subscription";
import { eq, and } from "drizzle-orm";

// POST /api/agents/:id/connect — 返回连接信息（含 Hub token）
// 认证：用户登录态（JWT）+ agent 归属验证
// 签名验证不在此处，由 Hub 的 Challenge-Response 完成
export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  // 检查订阅状态
  const subscription = await getActiveSubscription(user.userId);
  if (!subscription) {
    return NextResponse.json(
      { error: "subscription_required", message: "需要有效订阅才能连接节点" },
      { status: 403 }
    );
  }

  const { id } = await params;

  const agent = await db.query.agents.findFirst({
    where: and(eq(agents.id, id), eq(agents.userId, user.userId)),
    with: { hub: { columns: { httpUrl: true } } },
  });

  if (!agent) {
    return NextResponse.json({ error: "agent not found" }, { status: 404 });
  }

  if (!agent.paired) {
    return NextResponse.json({ error: "not_paired" }, { status: 403 });
  }

  if (!agent.online || !agent.hub) {
    return NextResponse.json({ error: "agent offline" }, { status: 503 });
  }

  // 签发 Hub 兼容 JWT，如果有公钥则嵌入（Hub 会做 Challenge-Response）
  const hubToken = await signHubUserToken(
    user.userId,
    user.username,
    user.role,
    agent.publicKey || undefined
  );

  // 合并 allowRelayTransfer：userOverrides > agent 默认值
  let allowRelayTransfer = agent.allowRelayTransfer;
  const overrides = await getUserOverrides(user.userId);
  if (overrides?.allowRelayTransfer != null) {
    allowRelayTransfer = overrides.allowRelayTransfer;
  }

  return NextResponse.json({
    hubHttpUrl: agent.hub.httpUrl,
    agentId: agent.id,
    hubToken,
    allowRelayTransfer,
  });
}
