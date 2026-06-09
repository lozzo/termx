import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { accessTokens } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { eq, and } from "drizzle-orm";

// GET /api/tokens/:id — 获取连接密钥详情
export async function GET(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id } = await params;
  const token = await db.query.accessTokens.findFirst({
    where: and(eq(accessTokens.id, id), eq(accessTokens.userId, user.userId)),
    with: {
      agents: {
        columns: {
          id: true,
          name: true,
          hostname: true,
          osInfo: true,
          online: true,
          lastSeen: true,
        },
      },
    },
  });

  if (!token) {
    return NextResponse.json({ error: "token not found" }, { status: 404 });
  }

  return NextResponse.json({
    id: token.id,
    name: token.name,
    token: token.token || "",
    expiresAt: token.expiresAt,
    createdAt: token.createdAt,
    agents: token.agents,
  });
}

// DELETE /api/tokens/:id — 删除连接密钥
export async function DELETE(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id } = await params;
  const [token] = await db
    .select()
    .from(accessTokens)
    .where(and(eq(accessTokens.id, id), eq(accessTokens.userId, user.userId)))
    .limit(1);

  if (!token) {
    return NextResponse.json({ error: "token not found" }, { status: 404 });
  }

  await db.delete(accessTokens).where(eq(accessTokens.id, id));
  return new NextResponse(null, { status: 204 });
}
