import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { userSnippets } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { getActiveSubscription } from "@/lib/subscription";
import { eq, desc } from "drizzle-orm";

// GET /api/user/snippets — 获取用户代码片段列表
export async function GET(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const snippets = await db
    .select()
    .from(userSnippets)
    .where(eq(userSnippets.userId, user.userId))
    .orderBy(desc(userSnippets.createdAt));

  return NextResponse.json(snippets);
}

// POST /api/user/snippets — 新建代码片段
export async function POST(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const subscription = await getActiveSubscription(user.userId);
  if (!subscription) {
    return NextResponse.json(
      { error: "subscription_required", message: "需要有效订阅才能创建代码片段" },
      { status: 403 }
    );
  }

  const body = await request.json();
  const { title, content } = body as { title?: string; content?: string };

  if (!title?.trim()) {
    return NextResponse.json({ error: "title is required" }, { status: 400 });
  }
  if (!content?.trim()) {
    return NextResponse.json({ error: "content is required" }, { status: 400 });
  }

  const [created] = await db
    .insert(userSnippets)
    .values({
      userId: user.userId,
      title: title.trim(),
      content: content.trim(),
    })
    .returning();

  return NextResponse.json(created);
}
