import { NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { db } from "@/lib/db";
import { orders } from "@/lib/schema";
import { createWechatPayment } from "@/lib/payment.service";
import { eq } from "drizzle-orm";

// POST /api/billing/orders/[id]/pay — 发起微信支付
export async function POST(
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
    const rawIp =
      request.headers.get("x-forwarded-for")?.split(",")[0]?.trim() ||
      request.headers.get("x-real-ip") ||
      undefined;

    // OMS-Pay only accepts pure IPv4, filter out IPv6 / mapped addresses
    const ipv4Re = /^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/;
    const clientIp = rawIp && ipv4Re.test(rawIp) ? rawIp : undefined;

    const result = await createWechatPayment(orderId, clientIp);

    return NextResponse.json({
      codeUrl: result.codeUrl,
      checkoutUrl: result.checkoutUrl,
      amount: result.amount,
      expireTime: result.expireTime,
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : "Payment failed";
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
