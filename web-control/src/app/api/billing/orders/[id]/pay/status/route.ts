import { NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { db } from "@/lib/db";
import { orders } from "@/lib/schema";
import { queryPaymentStatus } from "@/lib/payment.service";
import { eq } from "drizzle-orm";

// GET /api/billing/orders/[id]/pay/status — 查询支付状态
export async function GET(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id: orderId } = await params;

  const [order] = await db.select().from(orders).where(eq(orders.id, orderId)).limit(1);
  if (!order) {
    return NextResponse.json({ error: "order not found" }, { status: 404 });
  }
  if (order.userId !== user.userId) {
    return NextResponse.json({ error: "unauthorized" }, { status: 403 });
  }

  try {
    const result = await queryPaymentStatus(orderId);
    return NextResponse.json(result);
  } catch (err) {
    const message = err instanceof Error ? err.message : "Query failed";
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
