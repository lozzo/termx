import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { users, refreshTokens, referrals } from "@/lib/schema";
import { hashPassword } from "@/lib/password";
import { signAccessToken, setAuthCookies, isAppClient } from "@/lib/auth";
import { generateReferralCode } from "@/lib/referral.service";
import { eq, or } from "drizzle-orm";
import crypto from "crypto";
import bcrypt from "bcryptjs";

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const { username, email, password, referralCode } = body;

    if (!username || !email || !password) {
      return NextResponse.json(
        { error: "用户名、邮箱和密码为必填项" },
        { status: 400 }
      );
    }

    if (username.length < 3 || username.length > 32) {
      return NextResponse.json(
        { error: "用户名长度需在 3-32 个字符之间" },
        { status: 400 }
      );
    }

    if (password.length < 6) {
      return NextResponse.json(
        { error: "密码长度至少 6 个字符" },
        { status: 400 }
      );
    }

    // 校验邮箱格式
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    if (!emailRegex.test(email)) {
      return NextResponse.json(
        { error: "邮箱格式不正确" },
        { status: 400 }
      );
    }

    // 检查用户名或邮箱是否已存在
    const [existing] = await db
      .select()
      .from(users)
      .where(or(eq(users.username, username), eq(users.email, email)))
      .limit(1);

    if (existing) {
      const field = existing.username === username ? "用户名" : "邮箱";
      return NextResponse.json(
        { error: `该${field}已被注册` },
        { status: 409 }
      );
    }

    const passwordHash = await hashPassword(password);

    // 查找邀请人
    let referrerId: string | null = null;
    if (referralCode) {
      const [referrer] = await db
        .select({ id: users.id })
        .from(users)
        .where(eq(users.referralCode, referralCode))
        .limit(1);
      if (referrer) {
        referrerId = referrer.id;
      }
    }

    const newReferralCode = generateReferralCode();
    const [user] = await db
      .insert(users)
      .values({
        username,
        email,
        passwordHash,
        role: "user",
        referralCode: newReferralCode,
        referredBy: referrerId,
      })
      .returning();

    // 创建邀请记录
    if (referrerId) {
      await db.insert(referrals).values({
        referrerId,
        inviteeId: user.id,
        status: "pending",
      });
    }

    // 生成 access token
    const accessToken = await signAccessToken({
      userId: user.id,
      username: user.username,
      role: user.role,
    });

    // 生成 refresh token
    const plainRefreshToken = crypto.randomBytes(32).toString("hex");
    const refreshTokenHash = await bcrypt.hash(plainRefreshToken, 10);
    const tokenLookup = crypto.createHash("sha256").update(plainRefreshToken).digest("hex");

    await db.insert(refreshTokens).values({
      userId: user.id,
      tokenHash: refreshTokenHash,
      tokenLookup,
      deviceName: request.headers.get("user-agent")?.slice(0, 100) || "",
      expiresAt: new Date(Date.now() + 90 * 24 * 60 * 60 * 1000),
    });

    const userPayload = {
      id: user.id,
      username: user.username,
      email: user.email,
      role: user.role,
    };

    // App 客户端：返回 body tokens，不设 cookie
    if (isAppClient(request)) {
      return NextResponse.json({
        token: accessToken,
        refresh_token: plainRefreshToken,
        user: userPayload,
      });
    }

    // Web 客户端：设置 cookie
    await setAuthCookies(accessToken, plainRefreshToken);

    return NextResponse.json({
      user: userPayload,
    });
  } catch (error) {
    console.error("Register error:", error);
    return NextResponse.json({ error: "注册失败，请稍后重试" }, { status: 500 });
  }
}
