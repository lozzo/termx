import crypto from "crypto";
import { and, eq, gt, isNull, or } from "drizzle-orm";
import { db } from "./db";
import { accessTokens } from "./schema";
import { getHubSecret } from "./config";

export async function requireAccessToken(request: Request) {
  const authHeader = request.headers.get("authorization");
  if (!authHeader?.startsWith("Bearer ")) {
    return null;
  }
  const tokenPlain = authHeader.slice(7).trim();
  if (!tokenPlain) {
    return null;
  }
  const tokenHash = crypto.createHash("sha256").update(tokenPlain).digest("hex");
  return db.query.accessTokens.findFirst({
    where: and(
      eq(accessTokens.tokenHash, tokenHash),
      or(isNull(accessTokens.expiresAt), gt(accessTokens.expiresAt, new Date()))
    ),
  });
}

export async function createControlAccessToken(input: {
  userId: string;
  name: string;
  expiresAt?: Date | null;
}) {
  const tokenPlain = crypto.randomBytes(32).toString("hex");
  const tokenHash = crypto.createHash("sha256").update(tokenPlain).digest("hex");

  const [created] = await db
    .insert(accessTokens)
    .values({
      userId: input.userId,
      name: input.name.trim() || "TermX Node",
      tokenHash,
      token: tokenPlain,
      expiresAt: input.expiresAt ?? null,
    })
    .returning({ id: accessTokens.id, createdAt: accessTokens.createdAt });

  return {
    id: created.id,
    token: tokenPlain,
    createdAt: created.createdAt,
    expiresAt: input.expiresAt ?? null,
  };
}

export function isAuthorizedHub(request: Request): boolean {
  const expected = getHubSecret();
  const provided =
    request.headers.get("x-termx-hub-secret") ||
    request.headers.get("x-hub-secret");
  return Boolean(provided && provided === expected);
}

export function encodeAgentLabels(input: { labels?: string[] }): string {
  const labels = input.labels?.map((label) => label.trim()).filter(Boolean) ?? [];
  return labels.join(",");
}

export function asError(code: string, message: string) {
  return { error: { code, message } };
}
