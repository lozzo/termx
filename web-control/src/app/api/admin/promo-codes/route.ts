import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { promoCodes } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { getAdminPromoCodes } from "@/lib/queries";
import { invalidateAdminPromoCodes } from "@/lib/cache-system";
import { eq } from "drizzle-orm";

// GET /api/admin/promo-codes — 列出所有优惠码
export async function GET(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const result = await getAdminPromoCodes();
  return NextResponse.json(result);
}

// POST /api/admin/promo-codes — 创建优惠码
export async function POST(request: Request) {
  try {
    const admin = await requireAdmin(request);
    if (!admin) {
      return NextResponse.json({ error: "not found" }, { status: 404 });
    }

    const body = await request.json();
    const code = String(body.code || "").trim().toUpperCase();
    const discountType = body.discountType;
    const discountValue = Number(body.discountValue);
    const maxUses = body.maxUses === null || body.maxUses === undefined || body.maxUses === ""
      ? null
      : Number(body.maxUses);
    const startsAt = body.startsAt ? new Date(body.startsAt) : null;
    const expiresAt = body.expiresAt ? new Date(body.expiresAt) : null;

    if (!code || !discountType || !discountValue) {
      return NextResponse.json({ error: "code, discountType, discountValue required" }, { status: 400 });
    }

    if (discountType !== "fixed" && discountType !== "percent") {
      return NextResponse.json({ error: "discountType must be 'fixed' or 'percent'" }, { status: 400 });
    }

    if (!Number.isInteger(discountValue) || discountValue <= 0) {
      return NextResponse.json({ error: "discountValue must be a positive integer" }, { status: 400 });
    }

    if (discountType === "percent" && (discountValue < 1 || discountValue > 100)) {
      return NextResponse.json({ error: "percent discountValue must be 1-100" }, { status: 400 });
    }

    if (maxUses !== null && (!Number.isInteger(maxUses) || maxUses < 1)) {
      return NextResponse.json({ error: "maxUses must be a positive integer or null" }, { status: 400 });
    }

    if (startsAt && Number.isNaN(startsAt.getTime())) {
      return NextResponse.json({ error: "startsAt invalid" }, { status: 400 });
    }

    if (expiresAt && Number.isNaN(expiresAt.getTime())) {
      return NextResponse.json({ error: "expiresAt invalid" }, { status: 400 });
    }

    if (startsAt && expiresAt && expiresAt <= startsAt) {
      return NextResponse.json({ error: "expiresAt must be later than startsAt" }, { status: 400 });
    }

    const [existing] = await db
      .select({ id: promoCodes.id })
      .from(promoCodes)
      .where(eq(promoCodes.code, code))
      .limit(1);
    if (existing) {
      return NextResponse.json({ error: "code already exists" }, { status: 409 });
    }

    const [promo] = await db
      .insert(promoCodes)
      .values({
        code,
        discountType,
        discountValue,
        maxUses,
        startsAt,
        expiresAt,
      })
      .returning();

    await invalidateAdminPromoCodes();
    return NextResponse.json(promo, { status: 201 });
  } catch (error) {
    console.error("Create promo code error:", error);
    return NextResponse.json({ error: "create failed" }, { status: 500 });
  }
}
