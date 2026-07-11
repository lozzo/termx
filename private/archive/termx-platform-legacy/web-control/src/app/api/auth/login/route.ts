import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { users, refreshTokens } from "@/lib/schema";
import { verifyPassword } from "@/lib/password";
import { signAccessToken, setAuthCookies, isAppClient } from "@/lib/auth";
import { eq, and, lt } from "drizzle-orm";
import crypto from "crypto";
import bcrypt from "bcryptjs";

// 登录限速：5分钟内最多 10 次
const loginAttempts = new Map<string, { count: number; resetAt: number }>();
const RATE_LIMIT_WINDOW = 5 * 60 * 1000; // 5 minutes
const RATE_LIMIT_MAX = 10;

function checkRateLimit(ip: string): boolean {
  const now = Date.now();
  const entry = loginAttempts.get(ip);
  if (!entry || now > entry.resetAt) {
    loginAttempts.set(ip, { count: 1, resetAt: now + RATE_LIMIT_WINDOW });
    return true;
  }
  entry.count++;
  return entry.count <= RATE_LIMIT_MAX;
}

export async function POST(request: NextRequest) {
  try {
    const ip = request.headers.get("x-forwarded-for")?.split(",")[0]?.trim() || "unknown";
    if (!checkRateLimit(ip)) {
      return NextResponse.json(
        { error: "登录尝试次数过多，请稍后重试" },
        { status: 429 }
      );
    }

    const body = await request.json();
    const { username, password } = body;

    if (!username || !password) {
      return NextResponse.json(
        { error: "用户名和密码为必填项" },
        { status: 400 }
      );
    }

    const [user] = await db
      .select()
      .from(users)
      .where(eq(users.username, username))
      .limit(1);

    if (!user) {
      return NextResponse.json(
        { error: "用户名或密码错误" },
        { status: 401 }
      );
    }

    if (!user.passwordHash) {
      return NextResponse.json(
        { error: "该账户尚未设置本地密码，请先使用 GitHub 登录后前往账户设置设置密码" },
        { status: 403 }
      );
    }

    const valid = await verifyPassword(password, user.passwordHash);
    if (!valid) {
      return NextResponse.json(
        { error: "用户名或密码错误" },
        { status: 401 }
      );
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

    // 登录时清理该用户的过期 refresh token
    await db
      .delete(refreshTokens)
      .where(
        and(
          eq(refreshTokens.userId, user.id),
          lt(refreshTokens.expiresAt, new Date())
        )
      );

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
    console.error("Login error:", error);
    return NextResponse.json({ error: "登录失败，请稍后重试" }, { status: 500 });
  }
}
