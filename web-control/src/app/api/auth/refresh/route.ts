import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { refreshTokens } from "@/lib/schema";
import { signAccessToken, setAuthCookies, getRefreshTokenFromCookie, isAppClient, maybeRollRefreshToken } from "@/lib/auth";
import { eq, and, gt } from "drizzle-orm";
import bcrypt from "bcryptjs";
import crypto from "crypto";

export async function POST(request: NextRequest) {
  try {
    // 优先从 body 获取 refresh_token（App 客户端），否则从 cookie
    let plainToken: string | undefined;

    try {
      const body = await request.json();
      if (body?.refresh_token) {
        plainToken = body.refresh_token;
      }
    } catch {
      // body 解析失败（无 body），忽略
    }

    if (!plainToken) {
      plainToken = await getRefreshTokenFromCookie();
    }

    if (!plainToken) {
      return NextResponse.json(
        { error: "无有效的刷新令牌" },
        { status: 401 }
      );
    }

    // 使用 SHA-256 快速查找，再用 bcrypt 二次验证
    const tokenLookup = crypto.createHash("sha256").update(plainToken).digest("hex");

    const matchedToken = await db.query.refreshTokens.findFirst({
      where: and(
        eq(refreshTokens.tokenLookup, tokenLookup),
        gt(refreshTokens.expiresAt, new Date())
      ),
      with: { user: true },
    });

    if (!matchedToken || !(await bcrypt.compare(plainToken, matchedToken.tokenHash))) {
      return NextResponse.json(
        { error: "刷新令牌无效或已过期" },
        { status: 401 }
      );
    }

    const user = matchedToken.user;

    // 生成新 access token
    const accessToken = await signAccessToken({
      userId: user.id,
      username: user.username,
      role: user.role,
    });

    // 检查 refresh token 是否需要滚动续期
    const newRefreshToken = await maybeRollRefreshToken(
      matchedToken.id,
      user.id,
      matchedToken.deviceName,
      matchedToken.expiresAt
    );

    const userPayload = {
      id: user.id,
      username: user.username,
      email: user.email,
      role: user.role,
    };

    // App 客户端：返回 body token，不设 cookie
    if (isAppClient(request)) {
      return NextResponse.json({
        token: accessToken,
        user: userPayload,
        // 如果 refresh token 续期了，返回新的给客户端保存
        ...(newRefreshToken ? { refresh_token: newRefreshToken } : {}),
      });
    }

    // Web 客户端：设置 cookie
    if (newRefreshToken) {
      await setAuthCookies(accessToken, newRefreshToken);
    } else {
      await setAuthCookies(accessToken);
    }

    return NextResponse.json({
      user: userPayload,
    });
  } catch (error) {
    console.error("Refresh error:", error);
    return NextResponse.json({ error: "刷新令牌失败" }, { status: 500 });
  }
}
