import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { users, passwordResetCodes, refreshTokens } from "@/lib/schema";
import { hashPassword, verifyPassword } from "@/lib/password";
import { eq, and, gt, desc } from "drizzle-orm";

export async function POST(request: NextRequest) {
  try {
    const { email, code, newPassword } = await request.json();

    if (!email || !code || !newPassword) {
      return NextResponse.json(
        { error: "邮箱、验证码和新密码为必填项" },
        { status: 400 }
      );
    }

    if (newPassword.length < 6) {
      return NextResponse.json(
        { error: "密码长度至少 6 个字符" },
        { status: 400 }
      );
    }

    const [user] = await db
      .select()
      .from(users)
      .where(eq(users.email, email))
      .limit(1);

    if (!user) {
      return NextResponse.json(
        { error: "验证码无效或已过期" },
        { status: 400 }
      );
    }

    // 查找最近的未使用、未过期验证码
    const [resetCode] = await db
      .select()
      .from(passwordResetCodes)
      .where(
        and(
          eq(passwordResetCodes.userId, user.id),
          eq(passwordResetCodes.used, false),
          gt(passwordResetCodes.expiresAt, new Date())
        )
      )
      .orderBy(desc(passwordResetCodes.createdAt))
      .limit(1);

    if (!resetCode) {
      return NextResponse.json(
        { error: "验证码无效或已过期" },
        { status: 400 }
      );
    }

    // 检查尝试次数限制
    if (resetCode.attempts >= 5) {
      return NextResponse.json(
        { error: "验证码尝试次数过多，请重新获取" },
        { status: 400 }
      );
    }

    const isValid = await verifyPassword(code, resetCode.code);
    if (!isValid) {
      // 递增尝试次数
      await db
        .update(passwordResetCodes)
        .set({ attempts: resetCode.attempts + 1 })
        .where(eq(passwordResetCodes.id, resetCode.id));
      return NextResponse.json(
        { error: "验证码无效或已过期" },
        { status: 400 }
      );
    }

    // 标记验证码为已使用 & 更新密码
    const newPasswordHash = await hashPassword(newPassword);

    await db.transaction(async (tx) => {
      await tx
        .update(passwordResetCodes)
        .set({ used: true })
        .where(eq(passwordResetCodes.id, resetCode.id));
      await tx
        .update(users)
        .set({ passwordHash: newPasswordHash })
        .where(eq(users.id, user.id));
      // 作废该用户的所有 refresh token
      await tx
        .delete(refreshTokens)
        .where(eq(refreshTokens.userId, user.id));
    });

    return NextResponse.json({ message: "密码重置成功" });
  } catch (error) {
    console.error("Reset password error:", error);
    return NextResponse.json(
      { error: "重置密码失败，请稍后重试" },
      { status: 500 }
    );
  }
}
