import { NextResponse } from "next/server";
import { and, eq } from "drizzle-orm";
import { invalidateUserAgents } from "@/lib/cache-system";
import { getHubSecret } from "@/lib/config";
import { db } from "@/lib/db";
import { agents } from "@/lib/schema";
import {
  asError,
  requireAccessToken,
} from "@/lib/termx-control";

const pairTimeoutMs = 15000;

export async function POST(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const tokenRecord = await requireAccessToken(request);
  if (!tokenRecord) {
    return NextResponse.json(asError("unauthorized", "valid bearer access token is required"), { status: 401 });
  }

  const { id } = await params;
  const body = await readPairingBody(request);
  if (!body.ok) {
    return NextResponse.json(asError("invalid_pairing_claim", body.error), { status: 400 });
  }

  const agent = await db.query.agents.findFirst({
    where: and(eq(agents.id, id), eq(agents.userId, tokenRecord.userId)),
    with: {
      hub: {
        columns: {
          httpUrl: true,
          status: true,
        },
      },
    },
  });
  if (!agent) {
    return NextResponse.json(asError("machine_not_found", "machine is not owned by this account"), { status: 404 });
  }
  if (!agent.online || !agent.hub?.httpUrl) {
    return NextResponse.json(asError("machine_offline", "machine is not connected to a Hub"), { status: 409 });
  }

  const hubResponse = await fetchHubPairingClaim(agent.hub.httpUrl, {
    machine_id: id,
    pair_session_id: body.value.pair_session_id,
    pair_secret: body.value.pair_secret,
    app_device_id: body.value.app_device_id,
    app_name: body.value.app_name,
    requested_capabilities: body.value.requested_capabilities,
  });
  if (!hubResponse.ok) {
    return NextResponse.json(asError(hubResponse.code, hubResponse.message), { status: hubResponse.status });
  }

  await db
    .update(agents)
    .set({ paired: true })
    .where(eq(agents.id, id));
  await invalidateUserAgents(tokenRecord.userId);

  return NextResponse.json(hubResponse.body, { status: hubResponse.status });
}

async function readPairingBody(request: Request): Promise<
  | { ok: true; value: PairingBody }
  | { ok: false; error: string }
> {
  let value: unknown;
  try {
    value = await request.json();
  } catch {
    return { ok: false, error: "invalid JSON request body" };
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return { ok: false, error: "request body must be an object" };
  }
  const body = value as Record<string, unknown>;
  const pairSessionID = requiredBodyString(body, "pair_session_id");
  const pairSecret = requiredBodyString(body, "pair_secret");
  const appDeviceID = requiredBodyString(body, "app_device_id");
  const appName = requiredBodyString(body, "app_name");
  if (!pairSessionID || !pairSecret || !appDeviceID || !appName) {
    return { ok: false, error: "pair_session_id, pair_secret, app_device_id, and app_name are required" };
  }
  const requestedCapabilities = Array.isArray(body.requested_capabilities)
    ? body.requested_capabilities
        .filter((item): item is string => typeof item === "string" && item.trim() !== "")
        .map((item) => item.trim())
    : [];
  return {
    ok: true,
    value: {
      pair_session_id: pairSessionID,
      pair_secret: pairSecret,
      app_device_id: appDeviceID,
      app_name: appName,
      requested_capabilities: requestedCapabilities,
    },
  };
}

async function fetchHubPairingClaim(hubUrl: string, body: HubPairingClaimRequest): Promise<HubPairingClaimResponse> {
  const url = new URL("/api/internal/pairing/claims", normalizeHubURL(hubUrl));
  let response: Response;
  try {
    response = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-TermX-Hub-Secret": getHubSecret(),
      },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(pairTimeoutMs),
    });
  } catch (error) {
    return {
      ok: false,
      status: 502,
      code: "hub_unreachable",
      message: error instanceof Error ? error.message : String(error),
    };
  }

  const text = await response.text();
  const parsed = text.trim() ? parseJSON(text) : {};
  if (!response.ok) {
    const hubError = hubErrorMessage(parsed, response.statusText);
    return {
      ok: false,
      status: response.status,
      code: hubError.code,
      message: hubError.message,
    };
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return {
      ok: false,
      status: 502,
      code: "hub_invalid_response",
      message: "Hub pairing response must be an object",
    };
  }
  return { ok: true, status: response.status, body: parsed as Record<string, unknown> };
}

function requiredBodyString(body: Record<string, unknown>, key: string): string | null {
  const value = body[key];
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

function normalizeHubURL(raw: string): string {
  const trimmed = raw.trim();
  return trimmed.endsWith("/") ? trimmed : `${trimmed}/`;
}

function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

function hubErrorMessage(value: unknown, fallback: string): { code: string; message: string } {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const maybeError = (value as Record<string, unknown>).error;
    if (maybeError && typeof maybeError === "object" && !Array.isArray(maybeError)) {
      const err = maybeError as Record<string, unknown>;
      return {
        code: typeof err.code === "string" && err.code.trim() ? err.code.trim() : "hub_pairing_failed",
        message: typeof err.message === "string" && err.message.trim() ? err.message.trim() : fallback,
      };
    }
  }
  return { code: "hub_pairing_failed", message: fallback };
}

interface PairingBody {
  pair_session_id: string;
  pair_secret: string;
  app_device_id: string;
  app_name: string;
  requested_capabilities: string[];
}

interface HubPairingClaimRequest extends PairingBody {
  machine_id: string;
}

type HubPairingClaimResponse =
  | { ok: true; status: number; body: Record<string, unknown> }
  | { ok: false; status: number; code: string; message: string };
