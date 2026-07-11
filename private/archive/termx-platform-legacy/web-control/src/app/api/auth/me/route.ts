import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { users } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { getActiveSubscription } from "@/lib/subscription";
import { eq } from "drizzle-orm";

export async function GET(request: NextRequest) {
  try {
    const jwtUser = await getAuthFromRequest(request);

    if (!jwtUser) {
      return NextResponse.json({ error: "未登录" }, { status: 401 });
    }

    const [user] = await db
      .select({
        id: users.id,
        username: users.username,
        email: users.email,
        role: users.role,
        createdAt: users.createdAt,
        githubId: users.githubId,
        passwordHash: users.passwordHash,
      })
      .from(users)
      .where(eq(users.id, jwtUser.userId))
      .limit(1);

    if (!user) {
      return NextResponse.json({ error: "用户不存在" }, { status: 404 });
    }

    // 查询订阅状态
    const activeSub = await getActiveSubscription(jwtUser.userId);
    const subscription = activeSub
      ? {
          active: true,
          planName: activeSub.planName,
          currentPeriodEnd: activeSub.currentPeriodEnd,
        }
      : null;

    return NextResponse.json({
      user: {
        id: user.id,
        username: user.username,
        email: user.email,
        role: user.role,
        createdAt: user.createdAt,
        githubId: user.githubId,
        hasLocalPassword: Boolean(user.passwordHash),
      },
      subscription,
    });
  } catch (error) {
    console.error("Get me error:", error);
    return NextResponse.json({ error: "获取用户信息失败" }, { status: 500 });
  }
}
