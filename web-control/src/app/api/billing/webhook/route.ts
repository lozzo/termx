import { NextResponse } from "next/server";
import { OmsPayClient } from "@/lib/oms-pay-sdk";
import { getOmsPayWebhookSecret } from "@/lib/oms-pay-client";
import { handleOmsPayWebhook } from "@/lib/payment.service";
import type { WebhookPayload } from "@/lib/oms-pay-sdk";

// POST /api/billing/webhook — OMS-Pay Webhook 回调
export async function POST(request: Request) {
  const signature = request.headers.get("x-opay-signature");
  const timestamp = request.headers.get("x-opay-timestamp");

  if (!signature || !timestamp) {
    return NextResponse.json(
      { error: "Missing webhook signature headers" },
      { status: 400 }
    );
  }

  const rawBody = await request.text();

  // Verify webhook signature
  let secret: string;
  try {
    secret = getOmsPayWebhookSecret();
  } catch {
    console.error("[Webhook] Webhook secret not configured");
    return NextResponse.json(
      { error: "Webhook not configured" },
      { status: 500 }
    );
  }

  const isValid = await OmsPayClient.verifyWebhookAsync(
    rawBody,
    signature,
    timestamp,
    secret
  );

  if (!isValid) {
    console.warn("[Webhook] Invalid signature");
    return NextResponse.json(
      { error: "Invalid signature" },
      { status: 401 }
    );
  }

  const payload: WebhookPayload = JSON.parse(rawBody);

  try {
    await handleOmsPayWebhook(payload);
    return NextResponse.json({ ok: true });
  } catch (err) {
    console.error("[Webhook] Processing error:", err);
    return NextResponse.json(
      { error: "Webhook processing failed" },
      { status: 500 }
    );
  }
}
