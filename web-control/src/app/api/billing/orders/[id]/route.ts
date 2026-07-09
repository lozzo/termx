import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { orders } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { eq } from "drizzle-orm";

// GET /api/billing/orders/[id] — 获取订单详情
export async function GET(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id } = await params;

  const order = await db.query.orders.findFirst({
    where: eq(orders.id, id),
    with: { plan: true },
  });

  if (!order) {
    return NextResponse.json({ error: "order not found" }, { status: 404 });
  }
  if (order.userId !== user.userId) {
    return NextResponse.json({ error: "unauthorized" }, { status: 403 });
  }

  let features: string[] = [];
  try {
    features = JSON.parse(order.plan.features);
  } catch {
    // ignore
  }

  return NextResponse.json({
    id: order.id,
    planName: order.plan.name,
    amount: order.amount,
    originalAmount: order.amount + order.discountAmount,
    discountAmount: order.discountAmount,
    currency: order.currency,
    billingCycle: order.billingCycle,
    status: order.status,
    features,
    createdAt: order.createdAt,
    paidAt: order.paidAt,
  });
}
