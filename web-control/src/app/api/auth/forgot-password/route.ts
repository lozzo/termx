import { NextRequest, NextResponse } from "next/server";
import crypto from "crypto";
import { db } from "@/lib/db";
import { users, passwordResetCodes } from "@/lib/schema";
import { hashPassword } from "@/lib/password";
import { sendResetCode } from "@/lib/email";
import { eq, and, gt } from "drizzle-orm";

export async function POST(request: NextRequest) {
  try {
    const { email } = await request.json();

    if (!email) {
      return NextResponse.json({ error: "邮箱为必填项" }, { status: 400 });
    }

    // 无论用户是否存在都返回成功（防枚举）
    const [user] = await db
      .select()
      .from(users)
      .where(eq(users.email, email))
      .limit(1);

    if (user) {
      // 频率限制：60秒内只能发一次
      const [recentCode] = await db
        .select()
        .from(passwordResetCodes)
        .where(
          and(
            eq(passwordResetCodes.userId, user.id),
            gt(passwordResetCodes.createdAt, new Date(Date.now() - 60 * 1000))
          )
        )
        .limit(1);

      if (recentCode) {
        return NextResponse.json(
          { error: "请求过于频繁，请60秒后再试" },
          { status: 429 }
        );
      }

      // 生成6位数字验证码
      const code = crypto.randomInt(100000, 999999).toString();
      const codeHash = await hashPassword(code);

      await db.insert(passwordResetCodes).values({
        userId: user.id,
        code: codeHash,
        expiresAt: new Date(Date.now() + 10 * 60 * 1000), // 10分钟
      });

      await sendResetCode(email, code);
    }

    return NextResponse.json({ message: "如果该邮箱已注册，验证码已发送" });
  } catch (error) {
    console.error("Forgot password error:", error);
    return NextResponse.json(
      { error: "发送验证码失败，请稍后重试" },
      { status: 500 }
    );
  }
}
