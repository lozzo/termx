import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { promoCodes, promoCodeUsages, orders } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { eq } from "drizzle-orm";
import { invalidateAdminPromoCodes } from "@/lib/cache-system";

function parseDateInput(value: unknown, fieldName: string): Date | null {
  if (value === null || value === "" || value === undefined) {
    return null;
  }

  const date = new Date(String(value));
  if (Number.isNaN(date.getTime())) {
    throw new Error(`${fieldName} invalid`);
  }

  return date;
}

// GET /api/admin/promo-codes/[id] — 优惠码详情 + 使用记录
export async function GET(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const { id } = await params;

  const promo = await db.query.promoCodes.findFirst({
    where: eq(promoCodes.id, id),
    with: {
      usages: {
        with: { user: { columns: { username: true, email: true } } },
      },
    },
  });

  if (!promo) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  return NextResponse.json(promo);
}

// PATCH /api/admin/promo-codes/[id] — 修改优惠码
export async function PATCH(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  try {
    const admin = await requireAdmin(request);
    if (!admin) {
      return NextResponse.json({ error: "not found" }, { status: 404 });
    }

    const { id } = await params;
    const body = await request.json();

    const [existing] = await db
      .select()
      .from(promoCodes)
      .where(eq(promoCodes.id, id))
      .limit(1);

    if (!existing) {
      return NextResponse.json({ error: "not found" }, { status: 404 });
    }

    const [linkedOrder] = await db
      .select({ id: orders.id })
      .from(orders)
      .where(eq(orders.promoCodeId, id))
      .limit(1);
    const hasImmutableReferences = existing.usedCount > 0 || Boolean(linkedOrder);
    const updates: Record<string, unknown> = {};

    if (body.code !== undefined) {
      const code = String(body.code).trim().toUpperCase();
      if (!code) {
        return NextResponse.json({ error: "code required" }, { status: 400 });
      }
      if (hasImmutableReferences && code !== existing.code) {
        return NextResponse.json({ error: "used promo code cannot change code" }, { status: 400 });
      }
      const [conflict] = await db
        .select({ id: promoCodes.id })
        .from(promoCodes)
        .where(eq(promoCodes.code, code))
        .limit(1);
      if (conflict && conflict.id !== id) {
        return NextResponse.json({ error: "code already exists" }, { status: 409 });
      }
      updates.code = code;
    }

    if (body.discountType !== undefined) {
      if (body.discountType !== "fixed" && body.discountType !== "percent") {
        return NextResponse.json({ error: "discountType must be 'fixed' or 'percent'" }, { status: 400 });
      }
      if (hasImmutableReferences && body.discountType !== existing.discountType) {
        return NextResponse.json({ error: "used promo code cannot change discountType" }, { status: 400 });
      }
      updates.discountType = body.discountType;
    }

    if (body.discountValue !== undefined) {
      const discountValue = Number(body.discountValue);
      if (!Number.isInteger(discountValue) || discountValue <= 0) {
        return NextResponse.json({ error: "discountValue must be a positive integer" }, { status: 400 });
      }
      if (hasImmutableReferences && discountValue !== existing.discountValue) {
        return NextResponse.json({ error: "used promo code cannot change discountValue" }, { status: 400 });
      }
      updates.discountValue = discountValue;
    }

    const nextDiscountType = (updates.discountType as string | undefined) ?? existing.discountType;
    const nextDiscountValue = (updates.discountValue as number | undefined) ?? existing.discountValue;
    if (nextDiscountType === "percent" && (nextDiscountValue < 1 || nextDiscountValue > 100)) {
      return NextResponse.json({ error: "percent discountValue must be 1-100" }, { status: 400 });
    }

    if (body.active !== undefined) {
      if (typeof body.active !== "boolean") {
        return NextResponse.json({ error: "active must be boolean" }, { status: 400 });
      }
      updates.active = body.active;
    }

    if (body.maxUses !== undefined) {
      if (body.maxUses !== null) {
        const maxUses = Number(body.maxUses);
        if (!Number.isInteger(maxUses) || maxUses < 1) {
          return NextResponse.json({ error: "maxUses must be a positive integer or null" }, { status: 400 });
        }
        if (maxUses < existing.usedCount) {
          return NextResponse.json({ error: "maxUses cannot be less than usedCount" }, { status: 400 });
        }
        updates.maxUses = maxUses;
      } else {
        updates.maxUses = null;
      }
    }

    if (body.startsAt !== undefined) {
      updates.startsAt = parseDateInput(body.startsAt, "startsAt");
    }

    if (body.expiresAt !== undefined) {
      updates.expiresAt = parseDateInput(body.expiresAt, "expiresAt");
    }

    const nextStartsAt = (updates.startsAt as Date | null | undefined) ?? existing.startsAt;
    const nextExpiresAt = (updates.expiresAt as Date | null | undefined) ?? existing.expiresAt;
    if (nextStartsAt && nextExpiresAt && nextExpiresAt <= nextStartsAt) {
      return NextResponse.json({ error: "expiresAt must be later than startsAt" }, { status: 400 });
    }

    if (Object.keys(updates).length === 0) {
      return NextResponse.json({ error: "no valid fields to update" }, { status: 400 });
    }

    const [updated] = await db
      .update(promoCodes)
      .set(updates)
      .where(eq(promoCodes.id, id))
      .returning();

    await invalidateAdminPromoCodes();
    return NextResponse.json(updated);
  } catch (error) {
    const message = error instanceof Error ? error.message : "update failed";
    return NextResponse.json({ error: message }, { status: 400 });
  }
}

// DELETE /api/admin/promo-codes/[id] — 停用优惠码
export async function DELETE(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const { id } = await params;
  const [existing] = await db
    .select()
    .from(promoCodes)
    .where(eq(promoCodes.id, id))
    .limit(1);

  if (!existing) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const [[usageRef], [orderRef]] = await Promise.all([
    db.select({ id: promoCodeUsages.id }).from(promoCodeUsages).where(eq(promoCodeUsages.promoCodeId, id)).limit(1),
    db.select({ id: orders.id }).from(orders).where(eq(orders.promoCodeId, id)).limit(1),
  ]);

  if (usageRef || orderRef) {
    await db
      .update(promoCodes)
      .set({ active: false })
      .where(eq(promoCodes.id, id));

    await invalidateAdminPromoCodes();
    return NextResponse.json({ ok: true, deleted: false, deactivated: true });
  }

  await db.delete(promoCodes).where(eq(promoCodes.id, id));
  await invalidateAdminPromoCodes();
  return NextResponse.json({ ok: true, deleted: true, deactivated: false });
}
