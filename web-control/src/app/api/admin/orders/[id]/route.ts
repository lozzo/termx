import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { orders } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";

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

  if (body.status !== "paid") {
    return NextResponse.json({ error: "只能标记为已支付" }, { status: 400 });
  }

  const [updated] = await db
    .update(orders)
    .set({ status: "paid", paidAt: new Date() })
    .where(eq(orders.id, id))
    .returning();

  if (!updated) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  return NextResponse.json(updated);
}
