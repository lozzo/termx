import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { accessTokens, browserLoginCodes, users } from "@/lib/schema";
import { asError } from "@/lib/termx-control";

export async function POST(request: NextRequest) {
  const body = await request.json().catch(() => ({}));
  const browserLoginCode = typeof body.device_code === "string" ? body.device_code.trim() : "";
  if (!browserLoginCode) {
    return NextResponse.json(asError("invalid_request", "login code is required"), { status: 400 });
  }

  const code = await db.query.browserLoginCodes.findFirst({
    where: eq(browserLoginCodes.deviceCode, browserLoginCode),
  });

  if (!code) {
    return NextResponse.json(asError("invalid_grant", "login code is invalid"), { status: 400 });
  }

  if (code.expiresAt.getTime() <= Date.now()) {
    return NextResponse.json(asError("expired_token", "login code expired"), { status: 400 });
  }

  if (code.consumedAt) {
    return NextResponse.json(asError("invalid_grant", "login code was already consumed"), { status: 400 });
  }

  if (code.status !== "approved" || !code.userId || !code.accessTokenId) {
    return NextResponse.json(asError("authorization_pending", "authorization is still pending"), {
      status: 428,
    });
  }

  const [tokenRecord] = await db
    .select({ token: accessTokens.token })
    .from(accessTokens)
    .where(eq(accessTokens.id, code.accessTokenId))
    .limit(1);

  if (!tokenRecord?.token) {
    return NextResponse.json(asError("invalid_grant", "approved token was not found"), { status: 400 });
  }

  const [user] = await db
    .select({
      id: users.id,
      username: users.username,
      email: users.email,
      role: users.role,
    })
    .from(users)
    .where(eq(users.id, code.userId))
    .limit(1);

  if (!user) {
    return NextResponse.json(asError("user_not_found", "user was not found"), { status: 404 });
  }

  await db
    .update(browserLoginCodes)
    .set({ consumedAt: new Date() })
    .where(eq(browserLoginCodes.id, code.id));

  return NextResponse.json({
    token_type: "Bearer",
    access_token: tokenRecord.token,
    refresh_token: "",
    user,
  });
}
