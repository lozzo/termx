import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";
import { invalidateAdminAgents, invalidateUserAgents } from "@/lib/cache-system";

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

  if (body.pendingKick !== true) {
    return NextResponse.json({ error: "只支持踢出操作" }, { status: 400 });
  }

  const [updated] = await db
    .update(agents)
    .set({ pendingKick: true })
    .where(eq(agents.id, id))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await Promise.all([
    invalidateUserAgents(updated.userId),
    invalidateAdminAgents(),
  ]);

  return NextResponse.json({ id: updated.id, pendingKick: updated.pendingKick });
}
