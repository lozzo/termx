import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents, accessTokens } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { eq, and, or, gt, isNull } from "drizzle-orm";
import crypto from "crypto";

// GET /api/agents/:id/encrypted-key — 获取 agent 加密私钥
// 认证: Bearer token (用户 JWT 或 access token)
export async function GET(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const { id } = await params;

  // 尝试 JWT 认证（用户 cookie 或 Bearer JWT）
  let userId: string | null = null;

  const authHeader = request.headers.get("authorization");
  if (authHeader?.startsWith("Bearer ")) {
    const tokenPlain = authHeader.slice(7);

    // 先尝试作为 access token（SHA256 查表）
    const tokenHash = crypto.createHash("sha256").update(tokenPlain).digest("hex");
    const tokenRecord = await db.query.accessTokens.findFirst({
      where: and(
        eq(accessTokens.tokenHash, tokenHash),
        or(isNull(accessTokens.expiresAt), gt(accessTokens.expiresAt, new Date()))
      ),
    });

    if (tokenRecord) {
      userId = tokenRecord.userId;
    }
  }

  // 如果 access token 不匹配，尝试用户 JWT 认证
  if (!userId) {
    const user = await getAuthFromRequest(request);
    if (user) {
      userId = user.userId;
    }
  }

  if (!userId) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  // 查 agent，确认属于当前用户
  const agent = await db.query.agents.findFirst({
    where: and(eq(agents.id, id), eq(agents.userId, userId)),
  });

  if (!agent) {
    return NextResponse.json({ error: "agent not found" }, { status: 404 });
  }

  if (!agent.encryptedPrivateKey || !agent.keyNonce) {
    return NextResponse.json({ error: "encrypted key not available" }, { status: 404 });
  }

  return NextResponse.json({
    encrypted_private_key: agent.encryptedPrivateKey,
    key_nonce: agent.keyNonce,
  });
}
