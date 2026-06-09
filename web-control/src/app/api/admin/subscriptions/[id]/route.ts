import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { subscriptions } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";
import { invalidateAdminSubscriptions, invalidateSubscription } from "@/lib/cache-system";

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

  const sub = await db.query.subscriptions.findFirst({
    where: eq(subscriptions.id, id),
  });
  if (!sub) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const updates: Record<string, unknown> = {};

  // 延期
  if (typeof body.extendDays === "number" && body.extendDays > 0) {
    const base = sub.currentPeriodEnd > new Date() ? sub.currentPeriodEnd : new Date();
    updates.currentPeriodEnd = new Date(base.getTime() + body.extendDays * 24 * 60 * 60 * 1000);
    updates.status = "active";
  }

  // 取消
  if (body.status === "cancelled") {
    updates.status = "cancelled";
  }

  if (Object.keys(updates).length === 0) {
    return NextResponse.json({ error: "no valid fields to update" }, { status: 400 });
  }

  const [updated] = await db
    .update(subscriptions)
    .set(updates)
    .where(eq(subscriptions.id, id))
    .returning();

  await Promise.all([
    invalidateSubscription(sub.userId),
    invalidateAdminSubscriptions(),
  ]);

  return NextResponse.json(updated);
}
