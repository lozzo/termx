import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { userFnConfigs } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { getActiveSubscription } from "@/lib/subscription";
import { eq } from "drizzle-orm";

// GET /api/user/fn-config — 获取用户快捷键配置
export async function GET(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const row = await db.query.userFnConfigs.findFirst({
    where: eq(userFnConfigs.userId, user.userId),
  });

  const config = row ? JSON.parse(row.config) : { customItems: [], programOverrides: {}, customPrograms: [] };
  return NextResponse.json({ config });
}

// PUT /api/user/fn-config — 保存用户快捷键配置（upsert）
export async function PUT(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const subscription = await getActiveSubscription(user.userId);
  if (!subscription) {
    return NextResponse.json(
      { error: "subscription_required", message: "需要有效订阅才能保存快捷键配置" },
      { status: 403 }
    );
  }

  const body = await request.json();
  const { config } = body as { config: unknown };

  if (!config || typeof config !== "object") {
    return NextResponse.json({ error: "config is required" }, { status: 400 });
  }

  const configStr = JSON.stringify(config);

  const existing = await db.query.userFnConfigs.findFirst({
    where: eq(userFnConfigs.userId, user.userId),
  });

  if (existing) {
    await db
      .update(userFnConfigs)
      .set({ config: configStr, updatedAt: new Date() })
      .where(eq(userFnConfigs.userId, user.userId));
  } else {
    await db.insert(userFnConfigs).values({
      userId: user.userId,
      config: configStr,
    });
  }

  return NextResponse.json({ config });
}
