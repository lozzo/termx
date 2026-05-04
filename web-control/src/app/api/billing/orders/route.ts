import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { orders, plans, subscriptions } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { eq, desc } from "drizzle-orm";
import { validatePromoCode, applyPromoCode } from "@/lib/promo-code.service";
import { isBillingDisabled } from "@/lib/subscription";
import {
  invalidateAdminOrders,
  invalidateAdminSubscriptions,
  invalidateSubscription,
} from "@/lib/cache-system";

// GET /api/billing/orders — 列出当前用户的订单
export async function GET(request: Request) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const result = await db.query.orders.findMany({
    where: eq(orders.userId, user.userId),
    with: { plan: { columns: { name: true } } },
    orderBy: desc(orders.createdAt),
  });

  return NextResponse.json(
    result.map((o) => ({
      id: o.id,
      planName: o.plan.name,
      amount: o.amount,
      currency: o.currency,
      billingCycle: o.billingCycle,
      status: o.status,
      discountAmount: o.discountAmount,
      createdAt: o.createdAt,
      paidAt: o.paidAt,
    }))
  );
}

// POST /api/billing/orders — 创建订单
export async function POST(request: Request) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const body = await request.json();
  const { planId, billingCycle = "monthly", promoCode } = body;

  if (!planId) {
    return NextResponse.json({ error: "planId required" }, { status: 400 });
  }

  if (billingCycle !== "monthly" && billingCycle !== "yearly") {
    return NextResponse.json({ error: "billingCycle must be 'monthly' or 'yearly'" }, { status: 400 });
  }

  const [plan] = await db.select().from(plans).where(eq(plans.id, planId)).limit(1);
  if (!plan) {
    return NextResponse.json({ error: "plan not found" }, { status: 404 });
  }

  // 检查已有订阅，决定是否允许创建订单
  const existingSub = await db.query.subscriptions.findFirst({
    where: eq(subscriptions.userId, user.userId),
    with: { order: { columns: { billingCycle: true } } },
  });

  if (existingSub && existingSub.status === "active") {
    const currentCycle = existingSub.order.billingCycle;
    if (currentCycle === "yearly") {
      return NextResponse.json(
        { error: "您已是年付用户，无需重复购买" },
        { status: 400 }
      );
    }
    if (currentCycle === "monthly" && billingCycle === "monthly") {
      return NextResponse.json(
        { error: "您已是月付用户，如需升级请选择年付" },
        { status: 400 }
      );
    }
    // monthly → yearly: 允许升级
  }

  const originalAmount = billingCycle === "yearly" ? plan.priceYearly : plan.price;

  if (isBillingDisabled()) {
    const currentPeriodEnd = new Date("2999-12-31T23:59:59Z");
    const paidAt = new Date();
    const order = await db.transaction(async (tx) => {
      const [newOrder] = await tx
        .insert(orders)
        .values({
          userId: user.userId,
          planId,
          amount: 0,
          billingCycle,
          status: "paid",
          paymentMethod: "disabled",
          paidAt,
        })
        .returning();

      await tx
        .insert(subscriptions)
        .values({
          userId: user.userId,
          planId,
          orderId: newOrder.id,
          status: "active",
          currentPeriodEnd,
        })
        .onConflictDoUpdate({
          target: subscriptions.userId,
          set: {
            planId,
            orderId: newOrder.id,
            status: "active",
            currentPeriodEnd,
          },
        });

      return newOrder;
    });

    await Promise.all([
      invalidateSubscription(user.userId),
      invalidateAdminSubscriptions(),
      invalidateAdminOrders(),
    ]);

    return NextResponse.json(
      {
        id: order.id,
        amount: order.amount,
        originalAmount,
        discountAmount: order.discountAmount,
        status: order.status,
        paid: true,
      },
      { status: 201 }
    );
  }

  // 有优惠码：事务中验证 + 创建订单 + 记录使用
  if (promoCode) {
    try {
      const promo = await validatePromoCode(promoCode, user.userId, originalAmount);

      const order = await db.transaction(async (tx) => {
        const [newOrder] = await tx
          .insert(orders)
          .values({
            userId: user.userId,
            planId,
            amount: promo.finalAmount,
            billingCycle,
            promoCodeId: promo.promoCodeId,
            discountAmount: promo.discountAmount,
          })
          .returning();

        await applyPromoCode(tx, promo.promoCodeId, user.userId, newOrder.id, promo.discountAmount);
        return newOrder;
      });

      return NextResponse.json(
        {
          id: order.id,
          amount: order.amount,
          originalAmount,
          discountAmount: order.discountAmount,
          status: order.status,
        },
        { status: 201 }
      );
    } catch (err) {
      return NextResponse.json(
        { error: err instanceof Error ? err.message : "优惠码验证失败" },
        { status: 400 }
      );
    }
  }

  // 无优惠码：原有逻辑
  const [order] = await db
    .insert(orders)
    .values({
      userId: user.userId,
      planId,
      amount: originalAmount,
      billingCycle,
    })
    .returning();

  return NextResponse.json(
    { id: order.id, amount: order.amount, status: order.status },
    { status: 201 }
  );
}
