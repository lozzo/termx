import { NextResponse } from "next/server";
import { requireAdmin } from "@/lib/auth";
import { db } from "@/lib/db";
import { appReleases } from "@/lib/schema";
import { eq } from "drizzle-orm";
import { invalidateAdminAppReleases, invalidateAppLatestRelease } from "@/lib/cache-system";

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

  if (typeof body.forceUpdate === "boolean") updates.forceUpdate = body.forceUpdate;

  if (Object.keys(updates).length === 0) {
    return NextResponse.json({ error: "no valid fields" }, { status: 400 });
  }

  const [current] = await db
    .select()
    .from(appReleases)
    .where(eq(appReleases.id, Number(id)))
    .limit(1);
  if (!current) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await db.update(appReleases).set(updates).where(eq(appReleases.id, Number(id)));
  await Promise.all([
    invalidateAppLatestRelease(current.platform, current.type),
    invalidateAdminAppReleases(),
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
    .from(appReleases)
    .where(eq(appReleases.id, Number(id)))
    .limit(1);
  if (current) {
    await Promise.all([
      invalidateAppLatestRelease(current.platform, current.type),
      invalidateAdminAppReleases(),
    ]);
  }

  await db.delete(appReleases).where(eq(appReleases.id, Number(id)));
  return NextResponse.json({ ok: true });
}
