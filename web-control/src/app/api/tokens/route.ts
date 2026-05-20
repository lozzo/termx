import { NextRequest, NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { getActiveSubscription } from "@/lib/subscription";
import { getUserTokens } from "@/lib/queries";
import { createControlAccessToken } from "@/lib/termx-control";

// GET /api/tokens — 列出当前用户的连接密钥
export async function GET(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const tokens = await getUserTokens(user.userId);
  return NextResponse.json(tokens);
}

// POST /api/tokens — 创建连接密钥
export async function POST(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const subscription = await getActiveSubscription(user.userId);
  if (!subscription) {
    return NextResponse.json(
      { error: "subscription_required", message: "需要有效订阅才能创建连接密钥" },
      { status: 403 }
    );
  }

  // 检查设备数量上限
  const tokens = await getUserTokens(user.userId);
  if (tokens.length >= subscription.maxAgents) {
    return NextResponse.json(
      { error: "token_limit_exceeded", message: `已达到最大连接密钥数量限制（最多 ${subscription.maxAgents} 个）` },
      { status: 403 }
    );
  }

  const body = await request.json();
  const { name, expires_in } = body as { name?: string; expires_in?: string };

  if (!name || !name.trim()) {
    return NextResponse.json({ error: "name is required" }, { status: 400 });
  }

  // 计算过期时间
  let expiresAt: Date | null = null;
  switch (expires_in) {
    case "30d":
      expiresAt = new Date(Date.now() + 30 * 24 * 60 * 60 * 1000);
      break;
    case "90d":
      expiresAt = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000);
      break;
    case "1y":
      expiresAt = new Date(Date.now() + 365 * 24 * 60 * 60 * 1000);
      break;
    case "never":
      expiresAt = new Date("2999-12-31T23:59:59Z");
      break;
    default:
      expiresAt = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000);
      break;
  }

  const created = await createControlAccessToken({
    userId: user.userId,
    name: name.trim(),
    expiresAt,
  });

  return NextResponse.json({
    id: created.id,
    name: name.trim(),
    token: created.token,
    expiresAt: created.expiresAt,
    createdAt: created.createdAt,
  });
}
