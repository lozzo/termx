import { db } from "./db";
import { promoCodes, promoCodeUsages } from "./schema";
import { eq, and, sql } from "drizzle-orm";

export interface PromoValidationResult {
  promoCodeId: string;
  discountType: string;
  discountValue: number;
  discountAmount: number;
  finalAmount: number;
}

export async function validatePromoCode(
  code: string,
  userId: string,
  originalAmount: number
): Promise<PromoValidationResult> {
  const promo = await db.query.promoCodes.findFirst({
    where: eq(promoCodes.code, code.toUpperCase()),
  });

  if (!promo || !promo.active) {
    throw new Error("优惠码不存在或已失效");
  }

  const now = new Date();
  if (promo.startsAt && now < promo.startsAt) {
    throw new Error("优惠码尚未生效");
  }
  if (promo.expiresAt && now > promo.expiresAt) {
    throw new Error("优惠码已过期");
  }

  if (promo.maxUses !== null && promo.usedCount >= promo.maxUses) {
    throw new Error("优惠码已达使用上限");
  }

  const existingUsage = await db.query.promoCodeUsages.findFirst({
    where: and(
      eq(promoCodeUsages.promoCodeId, promo.id),
      eq(promoCodeUsages.userId, userId)
    ),
  });
  if (existingUsage) {
    throw new Error("您已使用过此优惠码");
  }

  let discountAmount: number;
  if (promo.discountType === "fixed") {
    discountAmount = promo.discountValue;
  } else {
    discountAmount = Math.floor(originalAmount * promo.discountValue / 100);
  }

  const finalAmount = Math.max(1, originalAmount - discountAmount);
  discountAmount = originalAmount - finalAmount;

  return {
    promoCodeId: promo.id,
    discountType: promo.discountType,
    discountValue: promo.discountValue,
    discountAmount,
    finalAmount,
  };
}

export async function applyPromoCode(
  tx: Pick<typeof db, "insert" | "update">,
  promoCodeId: string,
  userId: string,
  orderId: string,
  discountAmount: number
) {
  await tx.insert(promoCodeUsages).values({
    promoCodeId,
    userId,
    orderId,
    discountAmount,
  });

  await tx
    .update(promoCodes)
    .set({ usedCount: sql`${promoCodes.usedCount} + 1` })
    .where(eq(promoCodes.id, promoCodeId));
}
