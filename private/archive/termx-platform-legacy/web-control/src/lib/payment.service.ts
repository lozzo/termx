import { db } from "./db";
import { orders, plans, subscriptions } from "./schema";
import { getOmsPayClient } from "./oms-pay-client";
import type { OmsPayError, WebhookPayload } from "./oms-pay-sdk";
import { processReferralReward, applyPendingReferralRewards } from "./referral.service";
import { eq, and } from "drizzle-orm";
import { invalidateAdminOrders, invalidateAdminSubscriptions, invalidateSubscription } from "./cache-system";

// ─── Types ───────────────────────────────────────────────

interface CreatePaymentResult {
  orderId: string;
  codeUrl: string;
  checkoutUrl: string;
  amount: number;
  expireTime: string;
}

interface PaymentStatusResult {
  status: string;
  transactionId: string | null;
  successTime: string | null;
}

// ─── Create Wechat Payment ───────────────────────────────

export async function createWechatPayment(
  orderId: string,
  clientIp?: string
): Promise<CreatePaymentResult> {
  const order = await db.query.orders.findFirst({
    where: eq(orders.id, orderId),
    with: { plan: true },
  });

  if (!order) throw new Error("Order not found");
  if (order.status !== "pending") {
    throw new Error(`Order status is '${order.status}', expected 'pending'`);
  }

  const client = getOmsPayClient();

  // If order already has a paymentId, check if the existing payment is still usable
  if (order.paymentId) {
    try {
      const existing = await client.payments.get(order.paymentId);
      if (existing.status === "pending" && existing.codeUrl) {
        return {
          orderId: order.id,
          codeUrl: existing.codeUrl,
          checkoutUrl: existing.checkoutUrl,
          amount: existing.amount,
          expireTime: existing.expireAt,
        };
      }
    } catch {
      // Payment not found or error — proceed to create new one
    }
  }

  const payment = await client.payments.create({
    merchantOrderId: order.id,
    amount: order.amount,
    description: `${order.plan.name} - ${order.billingCycle === "yearly" ? "年付" : "月付"}`,
    paymentMethod: "wechat_native",
    currency: "CNY",
    ...(clientIp ? { clientIp } : {}),
    metadata: { orderId: order.id, userId: order.userId },
  });

  await db
    .update(orders)
    .set({
      paymentId: payment.paymentId,
      paymentMethod: "wechat_native",
    })
    .where(eq(orders.id, orderId));

  return {
    orderId: order.id,
    codeUrl: payment.codeUrl,
    checkoutUrl: payment.checkoutUrl,
    amount: payment.amount,
    expireTime: payment.expireAt,
  };
}

// ─── Query Payment Status ────────────────────────────────

export async function queryPaymentStatus(
  orderId: string
): Promise<PaymentStatusResult> {
  const [order] = await db.select().from(orders).where(eq(orders.id, orderId)).limit(1);
  if (!order) throw new Error("Order not found");

  // If already paid locally, return immediately
  if (order.status === "paid") {
    return {
      status: "paid",
      transactionId: null,
      successTime: order.paidAt?.toISOString() ?? null,
    };
  }

  if (!order.paymentId) {
    return { status: order.status, transactionId: null, successTime: null };
  }

  const client = getOmsPayClient();
  const detail = await client.payments.get(order.paymentId);

  // If OMS-Pay says paid but our order is still pending, sync it
  if (detail.status === "paid" && order.status === "pending") {
    await syncPaymentSuccess(orderId);
  }

  return {
    status: detail.status,
    transactionId: detail.providerTransactionId,
    successTime: detail.paidAt,
  };
}

// ─── Sync Payment Success (idempotent) ───────────────────

export async function syncPaymentSuccess(orderId: string): Promise<boolean> {
  const fulfilled = await db.transaction(async (tx) => {
    // Optimistic lock: only update if status is still pending
    const result = await tx
      .update(orders)
      .set({ status: "paid", paidAt: new Date() })
      .where(and(eq(orders.id, orderId), eq(orders.status, "pending")))
      .returning({ id: orders.id });

    if (result.length === 0) {
      console.log(`[Payment] Order ${orderId} already processed, skipping`);
      return null;
    }

    console.log(`[Payment] Order ${orderId} marked as paid`);

    const order = await tx.query.orders.findFirst({
      where: eq(orders.id, orderId),
      with: { plan: true },
    });

    if (order) {
      await handleOrderFulfillment(order, tx);
      return order;
    }

    return null;
  });

  // 事务提交后处理邀请奖励，确保能查到刚创建的订阅
  if (fulfilled) {
    try {
      await processReferralReward(fulfilled.userId);
      await applyPendingReferralRewards(fulfilled.userId);
    } catch (err) {
      console.error(`[Referral] Error processing referral rewards for user ${fulfilled.userId}:`, err);
    }
  }

  return fulfilled !== null;
}

// ─── Order Fulfillment (activate subscription) ───────────

async function handleOrderFulfillment(
  order: {
    id: string;
    userId: string;
    planId: string;
    billingCycle: string;
    plan: { name: string };
  },
  tx: Pick<typeof db, "insert"> = db
) {
  const now = new Date();
  const periodEnd = new Date(now);

  if (order.billingCycle === "yearly") {
    periodEnd.setFullYear(periodEnd.getFullYear() + 1);
  } else {
    periodEnd.setMonth(periodEnd.getMonth() + 1);
  }

  // userId is unique on Subscription, so upsert replaces any existing subscription
  await tx
    .insert(subscriptions)
    .values({
      userId: order.userId,
      planId: order.planId,
      orderId: order.id,
      status: "active",
      currentPeriodEnd: periodEnd,
    })
    .onConflictDoUpdate({
      target: subscriptions.userId,
      set: {
        planId: order.planId,
        orderId: order.id,
        status: "active",
        currentPeriodEnd: periodEnd,
      },
    });

  console.log(
    `[Payment] Subscription activated for user ${order.userId}, plan=${order.plan.name}, expires=${periodEnd.toISOString()}`
  );

  await Promise.all([
    invalidateSubscription(order.userId),
    invalidateAdminSubscriptions(),
    invalidateAdminOrders(),
  ]);
}

// ─── Webhook Handler ─────────────────────────────────────

export async function handleOmsPayWebhook(
  payload: WebhookPayload
): Promise<void> {
  const { event, merchantOrderId, amount } = payload;

  if (event === "payment.success") {
    const [order] = await db
      .select()
      .from(orders)
      .where(eq(orders.id, merchantOrderId))
      .limit(1);

    if (!order) {
      console.warn(
        `[Webhook] Order not found: ${merchantOrderId}`
      );
      return;
    }

    // Verify amount matches
    if (order.amount !== amount) {
      console.warn(
        `[Webhook] Amount mismatch for order ${merchantOrderId}: expected ${order.amount}, got ${amount}`
      );
      return;
    }

    await syncPaymentSuccess(merchantOrderId);
  } else if (event === "payment.closed") {
    await db
      .update(orders)
      .set({ status: "canceled" })
      .where(and(eq(orders.id, merchantOrderId), eq(orders.status, "pending")));
    console.log(`[Webhook] Order ${merchantOrderId} closed`);
  } else {
    console.log(`[Webhook] Unhandled event: ${event}`);
  }
}

// ─── Close Payment ───────────────────────────────────────

export async function closePayment(orderId: string): Promise<void> {
  // 原子更新：只有 pending 状态才能取消
  const result = await db
    .update(orders)
    .set({ status: "canceled" })
    .where(and(eq(orders.id, orderId), eq(orders.status, "pending")))
    .returning({ id: orders.id, paymentId: orders.paymentId });

  if (result.length === 0) {
    const [order] = await db.select().from(orders).where(eq(orders.id, orderId)).limit(1);
    if (!order) throw new Error("Order not found");
    throw new Error(`Cannot close order with status '${order.status}'`);
  }

  const paymentId = result[0].paymentId;
  if (paymentId) {
    const client = getOmsPayClient();
    try {
      await client.payments.close(paymentId);
    } catch (err) {
      const omsErr = err as OmsPayError;
      // Ignore if already closed/expired
      if (omsErr.code !== "VALIDATION_ERROR") throw err;
    }
  }

  console.log(`[Payment] Order ${orderId} closed`);
}

// ─── Cancel Order ────────────────────────────────────────

export async function cancelOrder(
  orderId: string,
  userId: string
): Promise<void> {
  const [order] = await db.select().from(orders).where(eq(orders.id, orderId)).limit(1);
  if (!order) throw new Error("Order not found");
  if (order.userId !== userId) throw new Error("Unauthorized");
  if (order.status !== "pending") {
    throw new Error(`Cannot cancel order with status '${order.status}'`);
  }

  await closePayment(orderId);
}
