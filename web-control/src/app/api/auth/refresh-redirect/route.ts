import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { refreshTokens } from "@/lib/schema";
import {
  signAccessToken,
  setAuthCookies,
  getRefreshTokenFromCookie,
  maybeRollRefreshToken,
} from "@/lib/auth";
import { eq, and, gt } from "drizzle-orm";
import bcrypt from "bcryptjs";
import crypto from "crypto";
import { buildRequestUrl, sanitizeRedirectPath } from "@/lib/url";

function buildLoginRedirectResponse(request: NextRequest, from: string) {
  const url = buildRequestUrl(request, "/login");
  url.searchParams.set("from", from);
  const response = NextResponse.redirect(url);
  response.cookies.delete("termx_access_token");
  response.cookies.delete("termx_refresh_token");
  return response;
}

/**
 * GET /api/auth/refresh-redirect?from=/dashboard/xxx
 * Middleware 在 access token 过期但 refresh token 存在时重定向到此端点。
 * 此端点在 Route Handler 中完成 token 刷新（可以安全地设置 cookies），
 * 然后重定向回原始页面。
 */
export async function GET(request: NextRequest) {
  const from = sanitizeRedirectPath(request.nextUrl.searchParams.get("from")) || "/dashboard";

  const plainToken = await getRefreshTokenFromCookie();
  if (!plainToken) {
    return buildLoginRedirectResponse(request, from);
  }

  try {
    const tokenLookup = crypto
      .createHash("sha256")
      .update(plainToken)
      .digest("hex");

    const matchedToken = await db.query.refreshTokens.findFirst({
      where: and(
        eq(refreshTokens.tokenLookup, tokenLookup),
        gt(refreshTokens.expiresAt, new Date())
      ),
      with: { user: true },
    });

    if (
      !matchedToken ||
      !(await bcrypt.compare(plainToken, matchedToken.tokenHash))
    ) {
      return buildLoginRedirectResponse(request, from);
    }

    const user = matchedToken.user;

    // 签发新 access token
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

    // 设置 cookies（Route Handler 中可以安全操作）
    if (newRefreshToken) {
      await setAuthCookies(accessToken, newRefreshToken);
    } else {
      await setAuthCookies(accessToken);
    }

    // 重定向回原始页面
    return NextResponse.redirect(buildRequestUrl(request, from));
  } catch (error) {
    console.error("Refresh redirect error:", error);
    return buildLoginRedirectResponse(request, from);
  }
}
