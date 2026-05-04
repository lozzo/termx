import crypto from "crypto";
import { and, eq, gt, isNull, or } from "drizzle-orm";
import { db } from "./db";
import { accessTokens, agents } from "./schema";
import { getHubSecret } from "./config";

export interface TermxTerminalMetadata {
  id: string;
  name?: string;
  command?: string[];
  cols?: number;
  rows?: number;
  state?: string;
}

export interface TermxAgentMetadata {
  termx?: {
    terminals?: TermxTerminalMetadata[];
  };
  raw?: string;
}

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

export async function findMachine(machineId: string) {
  return db.query.agents.findFirst({
    where: eq(agents.id, machineId.trim()),
    with: { hub: { columns: { id: true, httpUrl: true, status: true } } },
  });
}

export function encodeAgentMetadata(input: {
  labels?: string[];
  terminals?: TermxTerminalMetadata[];
}): string {
  const labels = input.labels?.map((label) => label.trim()).filter(Boolean) ?? [];
  const terminals = input.terminals?.filter((terminal) => terminal.id.trim()) ?? [];
  if (terminals.length === 0) {
    return labels.join(",");
  }
  return JSON.stringify({
    termx: {
      labels,
      terminals,
    },
  });
}

export function decodeAgentMetadata(raw: string | null | undefined): TermxAgentMetadata {
  const trimmed = raw?.trim() ?? "";
  if (!trimmed) {
    return {};
  }
  if (!trimmed.startsWith("{")) {
    return { raw: trimmed };
  }
  try {
    const parsed = JSON.parse(trimmed) as TermxAgentMetadata;
    if (parsed && typeof parsed === "object") {
      return parsed;
    }
  } catch {
    // Fall through to raw metadata.
  }
  return { raw: trimmed };
}

export function registeredTerminalIds(rawLabels: string | null | undefined): Set<string> {
  const metadata = decodeAgentMetadata(rawLabels);
  return new Set(
    (metadata.termx?.terminals ?? [])
      .map((terminal) => terminal.id.trim())
      .filter(Boolean)
  );
}

export function normalizeTerminalID(input: unknown): string {
  return typeof input === "string" ? input.trim() : "";
}

export function defaultTerminalID(rawLabels: string | null | undefined): string {
  const [first] = registeredTerminalIds(rawLabels);
  return first ?? "";
}

export function asError(code: string, message: string) {
  return { error: { code, message } };
}
