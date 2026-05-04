import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents, accessTokens } from "@/lib/schema";
import { getActiveSubscription } from "@/lib/subscription";
import { eq, and, or, gt, isNull } from "drizzle-orm";
import crypto from "crypto";
import { invalidateUserAgents } from "@/lib/cache-system";

// POST /api/agents/setup — Agent 一步注册（Bearer access token 认证）
export async function POST(request: NextRequest) {
  // 1. 从 Authorization: Bearer <token> 获取 access token
  const authHeader = request.headers.get("authorization");
  if (!authHeader?.startsWith("Bearer ")) {
    return NextResponse.json(
      { error: "Authorization: Bearer <token> required" },
      { status: 401 }
    );
  }
  const tokenPlain = authHeader.slice(7);
  const tokenHash = crypto.createHash("sha256").update(tokenPlain).digest("hex");

  // 2. 查 accessTokens 表
  const tokenRecord = await db.query.accessTokens.findFirst({
    where: and(
      eq(accessTokens.tokenHash, tokenHash),
      or(isNull(accessTokens.expiresAt), gt(accessTokens.expiresAt, new Date()))
    ),
  });

  if (!tokenRecord) {
    return NextResponse.json(
      { error: "invalid or expired access token" },
      { status: 401 }
    );
  }

  const userId = tokenRecord.userId;

  // 检查订阅状态
  const subscription = await getActiveSubscription(userId);
  if (!subscription) {
    return NextResponse.json(
      { error: "subscription_required", message: "需要有效订阅才能注册节点" },
      { status: 403 }
    );
  }

  // 3. 解析 body
  const body = await request.json();
  const {
    agent_id,
    public_key,
    name,
    hostname,
    os_info,
    labels,
  } = body;

  if (!agent_id || !public_key) {
    return NextResponse.json(
      { error: "agent_id and public_key are required" },
      { status: 400 }
    );
  }

  // 4. 验证 public_key 是 32 字节 base64
  try {
    const keyBytes = Buffer.from(public_key, "base64");
    if (keyBytes.length !== 32) {
      return NextResponse.json(
        { error: "public_key must be 32 bytes (Ed25519)" },
        { status: 400 }
      );
    }
  } catch {
    return NextResponse.json(
      { error: "invalid public_key encoding" },
      { status: 400 }
    );
  }

  // 5. 查 agent_id 是否存在
  const existing = await db.query.agents.findFirst({
    where: eq(agents.id, agent_id),
  });

  if (existing) {
    // 存在 → 更新
    await db
      .update(agents)
      .set({
        publicKey: public_key,
        userId,
        tokenId: tokenRecord.id,
        name: name || existing.name,
        hostname: hostname || existing.hostname,
        osInfo: os_info || existing.osInfo,
        labels: labels || existing.labels,
      })
      .where(eq(agents.id, agent_id));
  } else {
    // 不存在 → 检查数量上限后创建新记录
    const existingAgents = await db
      .select({ id: agents.id })
      .from(agents)
      .where(eq(agents.userId, userId));
    if (existingAgents.length >= subscription.maxAgents) {
      return NextResponse.json(
        { error: "agent_limit_exceeded", message: `已达到最大节点数量限制（最多 ${subscription.maxAgents} 个）` },
        { status: 403 }
      );
    }

    await db.insert(agents).values({
      id: agent_id,
      publicKey: public_key,
      userId,
      tokenId: tokenRecord.id,
      name: name || "",
      hostname: hostname || "",
      osInfo: os_info || "",
      labels: labels || "",
    });
  }

  await invalidateUserAgents(userId);

  return NextResponse.json({
    agent_id,
    status: "active",
    user_id: userId,
  });
}
