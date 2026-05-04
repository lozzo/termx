import crypto from "crypto";
import { NextResponse } from "next/server";
import { desc, eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { getAppUrl } from "@/lib/config";
import { agents } from "@/lib/schema";
import {
  asError,
  requireAccessToken,
} from "@/lib/termx-control";

export async function GET(request: Request) {
  const tokenRecord = await requireAccessToken(request);
  if (!tokenRecord) {
    return NextResponse.json(asError("unauthorized", "valid bearer access token is required"), { status: 401 });
  }

  const rows = await db.query.agents.findMany({
    where: eq(agents.userId, tokenRecord.userId),
    with: {
      hub: {
        columns: {
          id: true,
          name: true,
          region: true,
          httpUrl: true,
          status: true,
        },
      },
    },
    orderBy: desc(agents.lastSeen),
  });

  const controlUrl = getAppUrl() ?? requestOrigin(request);
  return NextResponse.json({
    control_url: controlUrl,
    machines: rows.map((agent) => ({
      id: agent.id,
      name: agent.name,
      hostname: agent.hostname,
      os_info: agent.osInfo,
      online: agent.online,
      paired: agent.paired,
      source: "cloud",
      public_key: agent.publicKey,
      machine_public_key_fingerprint: machinePublicKeyFingerprint(agent.publicKey),
      control_url: controlUrl,
      hub_id: agent.hubId,
      hub_http_url: agent.hub?.httpUrl ?? null,
      hub_status: agent.hub?.status ?? null,
      last_seen: agent.lastSeen?.toISOString() ?? null,
      created_at: agent.createdAt.toISOString(),
    })),
  });
}

function requestOrigin(request: Request): string {
  const url = new URL(request.url);
  return `${url.protocol}//${url.host}`;
}

function machinePublicKeyFingerprint(value: string | null): string | null {
  if (!value) return null;
  try {
    const publicKey = Buffer.from(value, "base64url");
    if (publicKey.length !== 32) return null;
    return `sha256:${crypto.createHash("sha256").update(publicKey).digest("hex")}`;
  } catch {
    return null;
  }
}
