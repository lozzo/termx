import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { subscriptions, orders } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";
import { getAdminSubscriptions } from "@/lib/queries";
import { invalidateAdminSubscriptions, invalidateSubscription } from "@/lib/cache-system";

export async function GET(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const result = await getAdminSubscriptions();
  return NextResponse.json(result);
}

// POST - 手动开通订阅
export async function POST(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const body = await request.json();
  const { userId, planId, days } = body;

  if (!userId || !planId || !days) {
    return NextResponse.json({ error: "userId, planId, days required" }, { status: 400 });
  }

  // 检查用户是否已有订阅
  const existing = await db.query.subscriptions.findFirst({
    where: eq(subscriptions.userId, userId),
  });
  if (existing) {
    return NextResponse.json({ error: "用户已有订阅，请使用延期功能" }, { status: 400 });
  }

  const currentPeriodEnd = new Date(Date.now() + days * 24 * 60 * 60 * 1000);

  // 创建一个管理员手动订单
  const [order] = await db
    .insert(orders)
    .values({
      userId,
      planId,
      amount: 0,
      status: "paid",
      paymentMethod: "admin_manual",
      paidAt: new Date(),
    })
    .returning();

  const [sub] = await db
    .insert(subscriptions)
    .values({
      userId,
      planId,
      orderId: order.id,
      status: "active",
      currentPeriodEnd,
    })
    .returning();

  await Promise.all([
    invalidateSubscription(userId),
    invalidateAdminSubscriptions(),
  ]);

  return NextResponse.json(sub, { status: 201 });
}
