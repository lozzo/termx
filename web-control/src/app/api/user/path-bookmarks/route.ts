import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { userPathBookmarks } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { getActiveSubscription } from "@/lib/subscription";
import { eq, and, desc } from "drizzle-orm";

// GET /api/user/path-bookmarks?agentId=xxx — 获取用户路径收藏列表（可按 agent 过滤）
export async function GET(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const agentId = request.nextUrl.searchParams.get("agentId");

  const conditions = [eq(userPathBookmarks.userId, user.userId)];
  if (agentId) {
    conditions.push(eq(userPathBookmarks.agentId, agentId));
  }

  const bookmarks = await db
    .select()
    .from(userPathBookmarks)
    .where(and(...conditions))
    .orderBy(desc(userPathBookmarks.createdAt));

  return NextResponse.json(bookmarks);
}

// POST /api/user/path-bookmarks — 新建路径收藏
export async function POST(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const subscription = await getActiveSubscription(user.userId);
  if (!subscription) {
    return NextResponse.json(
      { error: "subscription_required", message: "需要有效订阅才能创建路径收藏" },
      { status: 403 }
    );
  }

  const body = await request.json();
  const { path, name, agentId } = body as { path?: string; name?: string; agentId?: string };

  if (!path?.trim()) {
    return NextResponse.json({ error: "path is required" }, { status: 400 });
  }
  if (!name?.trim()) {
    return NextResponse.json({ error: "name is required" }, { status: 400 });
  }
  if (!agentId?.trim()) {
    return NextResponse.json({ error: "agentId is required" }, { status: 400 });
  }

  const [created] = await db
    .insert(userPathBookmarks)
    .values({
      userId: user.userId,
      agentId: agentId.trim(),
      path: path.trim(),
      name: name.trim(),
    })
    .returning();

  return NextResponse.json(created);
}
