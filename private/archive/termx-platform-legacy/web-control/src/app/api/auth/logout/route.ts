import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { refreshTokens } from "@/lib/schema";
import { clearAuthCookies, getRefreshTokenFromCookie } from "@/lib/auth";
import { eq } from "drizzle-orm";
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

    if (plainToken) {
      // 使用 SHA-256 快速定位并删除
      const tokenLookup = crypto.createHash("sha256").update(plainToken).digest("hex");
      await db
        .delete(refreshTokens)
        .where(eq(refreshTokens.tokenLookup, tokenLookup));
    }

    await clearAuthCookies();

    return NextResponse.json({ ok: true });
  } catch (error) {
    console.error("Logout error:", error);
    // 即使出错也清除 cookies
    await clearAuthCookies();
    return NextResponse.json({ ok: true });
  }
}
