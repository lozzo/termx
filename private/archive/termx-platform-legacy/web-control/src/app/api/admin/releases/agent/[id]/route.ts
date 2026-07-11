import { NextResponse } from "next/server";
import { requireAdmin } from "@/lib/auth";
import { db } from "@/lib/db";
import { agentReleases } from "@/lib/schema";
import { eq } from "drizzle-orm";
import { invalidateAdminAgentReleases, invalidateAgentLatestRelease } from "@/lib/cache-system";

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

  if (typeof body.active === "boolean") updates.active = body.active;
  if (typeof body.forceUpdate === "boolean") updates.forceUpdate = body.forceUpdate;

  if (Object.keys(updates).length === 0) {
    return NextResponse.json({ error: "no valid fields" }, { status: 400 });
  }

  const [current] = await db
    .select()
    .from(agentReleases)
    .where(eq(agentReleases.id, Number(id)))
    .limit(1);
  if (!current) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await db.update(agentReleases).set(updates).where(eq(agentReleases.id, Number(id)));
  await Promise.all([
    invalidateAgentLatestRelease(current.os, current.arch),
    invalidateAdminAgentReleases(),
  ]);
  return NextResponse.json({ ok: true });
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
  const [current] = await db
    .select()
    .from(agentReleases)
    .where(eq(agentReleases.id, Number(id)))
    .limit(1);
  if (current) {
    await Promise.all([
      invalidateAgentLatestRelease(current.os, current.arch),
      invalidateAdminAgentReleases(),
    ]);
  }

  await db.delete(agentReleases).where(eq(agentReleases.id, Number(id)));
  return NextResponse.json({ ok: true });
}
