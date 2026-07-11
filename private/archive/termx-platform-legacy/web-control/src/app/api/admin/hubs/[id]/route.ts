import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { hubs, agents } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq, count } from "drizzle-orm";
import { invalidateAdminHubs } from "@/lib/cache-system";

export async function PATCH(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const { id } = await params;
  const body = await request.json();

  const updates: Record<string, unknown> = {};
  if (typeof body.name === "string") updates.name = body.name;
  if (typeof body.region === "string") updates.region = body.region;
  if (typeof body.httpUrl === "string") updates.httpUrl = body.httpUrl;
  if (typeof body.grpcUrl === "string") updates.grpcUrl = body.grpcUrl;
  if (typeof body.status === "string") updates.status = body.status;

  if (Object.keys(updates).length === 0) {
    return NextResponse.json({ error: "no valid fields to update" }, { status: 400 });
  }

  const [updated] = await db
    .update(hubs)
    .set(updates)
    .where(eq(hubs.id, id))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await invalidateAdminHubs();
  return NextResponse.json(updated);
}

export async function DELETE(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const { id } = await params;

  // 检查是否有 Agent 关联
  const [agentCount] = await db
    .select({ count: count() })
    .from(agents)
    .where(eq(agents.hubId, id));

  if (agentCount.count > 0) {
    return NextResponse.json(
      { error: `该 Hub 下还有 ${agentCount.count} 个 Agent，无法删除` },
      { status: 400 }
    );
  }

  const [deleted] = await db
    .delete(hubs)
    .where(eq(hubs.id, id))
    .returning();

  if (!deleted) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await invalidateAdminHubs();
  return NextResponse.json({ ok: true });
}
