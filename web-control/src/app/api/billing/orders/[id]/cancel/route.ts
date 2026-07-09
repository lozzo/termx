import { NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { cancelOrder } from "@/lib/payment.service";

// POST /api/billing/orders/[id]/cancel — 取消订单
export async function POST(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id: orderId } = await params;

  try {
    await cancelOrder(orderId, user.userId);
    return NextResponse.json({ ok: true });
  } catch (err) {
    const message = err instanceof Error ? err.message : "Cancel failed";
    const status = message === "Order not found" ? 404 : message === "Unauthorized" ? 403 : 400;
    return NextResponse.json({ error: message }, { status });
  }
}
