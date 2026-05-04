import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { userSnippets } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { eq, and } from "drizzle-orm";

// PUT /api/user/snippets/[id] — 更新代码片段
export async function PUT(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id } = await params;
  const body = await request.json();
  const { title, content } = body as { title?: string; content?: string };

  const patch: Record<string, string> = {};
  if (title !== undefined) patch.title = title.trim();
  if (content !== undefined) patch.content = content.trim();

  if (Object.keys(patch).length === 0) {
    return NextResponse.json({ error: "nothing to update" }, { status: 400 });
  }

  const [updated] = await db
    .update(userSnippets)
    .set(patch)
    .where(and(eq(userSnippets.id, id), eq(userSnippets.userId, user.userId)))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  return NextResponse.json(updated);
}

// DELETE /api/user/snippets/[id] — 删除代码片段
export async function DELETE(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id } = await params;

  const [deleted] = await db
    .delete(userSnippets)
    .where(and(eq(userSnippets.id, id), eq(userSnippets.userId, user.userId)))
    .returning({ id: userSnippets.id });

  if (!deleted) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  return new NextResponse(null, { status: 204 });
}
