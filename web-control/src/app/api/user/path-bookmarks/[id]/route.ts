import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { userPathBookmarks } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { eq, and } from "drizzle-orm";

// PUT /api/user/path-bookmarks/[id] — 更新路径收藏
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
  const { name, path } = body as { name?: string; path?: string };

  const patch: Record<string, string> = {};
  if (name !== undefined) patch.name = name.trim();
  if (path !== undefined) patch.path = path.trim();

  if (Object.keys(patch).length === 0) {
    return NextResponse.json({ error: "nothing to update" }, { status: 400 });
  }

  const [updated] = await db
    .update(userPathBookmarks)
    .set(patch)
    .where(and(eq(userPathBookmarks.id, id), eq(userPathBookmarks.userId, user.userId)))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  return NextResponse.json(updated);
}

// DELETE /api/user/path-bookmarks/[id] — 删除路径收藏
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
    .delete(userPathBookmarks)
    .where(and(eq(userPathBookmarks.id, id), eq(userPathBookmarks.userId, user.userId)))
    .returning({ id: userPathBookmarks.id });

  if (!deleted) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  return new NextResponse(null, { status: 204 });
}
