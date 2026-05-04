import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { userOverrides, users } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { getAdminUserOverrides } from "@/lib/queries";
import { invalidateAdminOverrides, invalidateSubscription } from "@/lib/cache-system";
import { eq } from "drizzle-orm";

export async function GET(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const result = await getAdminUserOverrides();
  return NextResponse.json(result);
}

export async function POST(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const body = await request.json();
  const { userId, overrides, note, expiresAt } = body;

  if (!userId) {
    return NextResponse.json({ error: "userId required" }, { status: 400 });
  }

  // 检查用户是否存在
  const user = await db.query.users.findFirst({
    where: eq(users.id, userId),
  });
  if (!user) {
    return NextResponse.json({ error: "用户不存在" }, { status: 400 });
  }

  // 检查是否已有 override
  const existing = await db.query.userOverrides.findFirst({
    where: eq(userOverrides.userId, userId),
  });
  if (existing) {
    return NextResponse.json({ error: "该用户已有特权配置，请编辑现有记录" }, { status: 400 });
  }

  const [created] = await db
    .insert(userOverrides)
    .values({
      userId,
      overrides: overrides || {},
      note: note || null,
      expiresAt: expiresAt ? new Date(expiresAt) : null,
    })
    .returning();

  await Promise.all([
    invalidateSubscription(userId),
    invalidateAdminOverrides(),
  ]);

  return NextResponse.json(created, { status: 201 });
}
