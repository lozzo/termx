import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { plans } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";
import { invalidatePlans } from "@/lib/cache-system";

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
  if (typeof body.price === "number") updates.price = body.price;
  if (typeof body.priceYearly === "number") updates.priceYearly = body.priceYearly;
  if (typeof body.features === "string") updates.features = body.features;
  if (typeof body.maxServers === "number") updates.maxServers = body.maxServers;
  if (typeof body.maxAgents === "number") updates.maxAgents = body.maxAgents;
  if (typeof body.active === "boolean") updates.active = body.active;

  if (Object.keys(updates).length === 0) {
    return NextResponse.json({ error: "no valid fields to update" }, { status: 400 });
  }

  const [updated] = await db
    .update(plans)
    .set(updates)
    .where(eq(plans.id, id))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  await invalidatePlans();
  return NextResponse.json(updated);
}
