import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { users, refreshTokens } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { verifyPassword, hashPassword } from "@/lib/password";
import { eq } from "drizzle-orm";

export async function PUT(request: NextRequest) {
  try {
    const jwtUser = await getAuthFromRequest(request);
    if (!jwtUser) {
      return NextResponse.json({ error: "未登录" }, { status: 401 });
    }

    const body = await request.json();
    const { currentPassword, newPassword } = body;

    if (!newPassword) {
      return NextResponse.json({ error: "新密码为必填项" }, { status: 400 });
    }

    if (newPassword.length < 6) {
      return NextResponse.json({ error: "新密码至少 6 个字符" }, { status: 400 });
    }

    const [user] = await db
      .select({
        id: users.id,
        passwordHash: users.passwordHash,
      })
      .from(users)
      .where(eq(users.id, jwtUser.userId))
      .limit(1);

    if (!user) {
      return NextResponse.json({ error: "用户不存在" }, { status: 404 });
    }

    if (user.passwordHash) {
      if (!currentPassword) {
        return NextResponse.json({ error: "当前密码和新密码为必填项" }, { status: 400 });
      }

      const valid = await verifyPassword(currentPassword, user.passwordHash);
      if (!valid) {
        return NextResponse.json({ error: "当前密码错误" }, { status: 401 });
      }
    }

    const newHash = await hashPassword(newPassword);
    await db
      .update(users)
      .set({ passwordHash: newHash })
      .where(eq(users.id, jwtUser.userId));

    // 作废该用户的所有 refresh token
    await db.delete(refreshTokens).where(eq(refreshTokens.userId, jwtUser.userId));

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error("Change password error:", error);
    return NextResponse.json({ error: "修改密码失败" }, { status: 500 });
  }
}
