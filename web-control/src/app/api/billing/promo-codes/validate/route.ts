import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { plans } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { validatePromoCode } from "@/lib/promo-code.service";
import { eq } from "drizzle-orm";

export async function POST(request: Request) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const body = await request.json();
  const { code, planId, billingCycle = "monthly" } = body;

  if (!code || !planId) {
    return NextResponse.json({ error: "code and planId required" }, { status: 400 });
  }

  const [plan] = await db.select().from(plans).where(eq(plans.id, planId)).limit(1);
  if (!plan) {
    return NextResponse.json({ error: "plan not found" }, { status: 404 });
  }

  const originalAmount = billingCycle === "yearly" ? plan.priceYearly : plan.price;

  try {
    const result = await validatePromoCode(code, user.userId, originalAmount);
    return NextResponse.json({
      valid: true,
      discountType: result.discountType,
      discountValue: result.discountValue,
      discountAmount: result.discountAmount,
      finalAmount: result.finalAmount,
    });
  } catch (err) {
    return NextResponse.json({
      valid: false,
      error: err instanceof Error ? err.message : "优惠码验证失败",
    });
  }
}
