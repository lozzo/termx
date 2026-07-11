import crypto from "crypto";
import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { browserLoginCodes } from "@/lib/schema";
import { buildRequestUrl } from "@/lib/url";

const BROWSER_LOGIN_TTL_SECONDS = 5 * 60;
const POLL_INTERVAL_SECONDS = 2;
const USER_CODE_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";

function randomUserCode(): string {
  let raw = "";
  for (let i = 0; i < 8; i++) {
    raw += USER_CODE_ALPHABET[crypto.randomInt(USER_CODE_ALPHABET.length)];
  }
  return `${raw.slice(0, 4)}-${raw.slice(4)}`;
}

async function createUniqueUserCode(): Promise<string> {
  for (let i = 0; i < 5; i++) {
    const userCode = randomUserCode();
    const existing = await db.query.browserLoginCodes.findFirst({
      where: (codes, { eq }) => eq(codes.userCode, userCode),
    });
    if (!existing) {
      return userCode;
    }
  }
  return `${crypto.randomBytes(2).toString("hex").toUpperCase()}-${crypto.randomBytes(2).toString("hex").toUpperCase()}`;
}

export async function POST(request: NextRequest) {
  const body = await request.json().catch(() => ({}));
  const clientName =
    typeof body.client_name === "string" && body.client_name.trim()
      ? body.client_name.trim().slice(0, 80)
      : "TermX CLI";

  const browserLoginCode = crypto.randomBytes(32).toString("base64url");
  const userCode = await createUniqueUserCode();
  const expiresAt = new Date(Date.now() + BROWSER_LOGIN_TTL_SECONDS * 1000);
  const verificationUri = buildRequestUrl(request, "/device-login");
  const verificationUriComplete = buildRequestUrl(
    request,
    `/device-login?code=${encodeURIComponent(userCode)}`
  );

  await db.insert(browserLoginCodes).values({
    deviceCode: browserLoginCode,
    userCode,
    clientName,
    expiresAt,
  });

  return NextResponse.json({
    device_code: browserLoginCode,
    user_code: userCode,
    verification_uri: verificationUri.toString(),
    verification_uri_complete: verificationUriComplete.toString(),
    expires_in: BROWSER_LOGIN_TTL_SECONDS,
    interval: POLL_INTERVAL_SECONDS,
  });
}
