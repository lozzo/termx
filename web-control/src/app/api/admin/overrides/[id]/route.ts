import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { userOverrides } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";
import { invalidateAdminOverrides, invalidateSubscription } from "@/lib/cache-system";

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
  if (body.overrides !== undefined) updates.overrides = body.overrides;
  if (body.note !== undefined) updates.note = body.note || null;
  if (body.expiresAt !== undefined) updates.expiresAt = body.expiresAt ? new Date(body.expiresAt) : null;
  updates.updatedAt = new Date();

  const [updated] = await db
    .update(userOverrides)
    .set(updates)
    .where(eq(userOverrides.id, id))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await Promise.all([
    invalidateSubscription(updated.userId),
    invalidateAdminOverrides(),
  ]);

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

  const [deleted] = await db
    .delete(userOverrides)
    .where(eq(userOverrides.id, id))
    .returning();

  if (!deleted) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await Promise.all([
    invalidateSubscription(deleted.userId),
    invalidateAdminOverrides(),
  ]);

  return NextResponse.json({ ok: true });
}
