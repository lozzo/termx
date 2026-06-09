import { NextRequest, NextResponse } from "next/server";
import { and, eq, gt, isNull } from "drizzle-orm";
import { db } from "@/lib/db";
import { getAuthFromRequest } from "@/lib/auth";
import { browserLoginCodes } from "@/lib/schema";
import { asError, createControlAccessToken } from "@/lib/termx-control";

function normalizeUserCode(input: unknown): string {
  if (typeof input !== "string") return "";
  const compact = input.trim().toUpperCase().replace(/[^A-Z0-9]/g, "");
  if (compact.length !== 8) return input.trim().toUpperCase();
  return `${compact.slice(0, 4)}-${compact.slice(4)}`;
}

export async function POST(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json(asError("unauthorized", "login is required"), { status: 401 });
  }

  const body = await request.json().catch(() => ({}));
  const userCode = normalizeUserCode(body.user_code ?? body.code);
  if (!userCode) {
    return NextResponse.json(asError("invalid_code", "authorization code is required"), { status: 400 });
  }

  const existing = await db.query.browserLoginCodes.findFirst({
    where: and(
      eq(browserLoginCodes.userCode, userCode),
      eq(browserLoginCodes.status, "pending"),
      isNull(browserLoginCodes.consumedAt),
      gt(browserLoginCodes.expiresAt, new Date())
    ),
  });

  if (!existing) {
    return NextResponse.json(asError("code_not_found", "authorization code is invalid or expired"), {
      status: 404,
    });
  }

  const token = await createControlAccessToken({
    userId: user.userId,
    name: `TermX ${existing.clientName || "CLI"}`,
    expiresAt: null,
  });

  await db
    .update(browserLoginCodes)
    .set({
      status: "approved",
      userId: user.userId,
      accessTokenId: token.id,
      approvedAt: new Date(),
    })
    .where(eq(browserLoginCodes.id, existing.id));

  return NextResponse.json({
    ok: true,
    user_code: userCode,
    client_name: existing.clientName,
  });
}
