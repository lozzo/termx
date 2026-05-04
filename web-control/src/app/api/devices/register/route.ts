import { NextRequest, NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { agents } from "@/lib/schema";
import { getActiveSubscription } from "@/lib/subscription";
import {
  asError,
  encodeAgentLabels,
  requireAccessToken,
} from "@/lib/termx-control";
import { invalidateUserAgents } from "@/lib/cache-system";

interface DeviceRegistrationBody {
  deviceId?: string;
  machinePublicKey?: string;
  displayName?: string;
  hostname?: string;
  platform?: string;
  state?: string;
  hubId?: string;
  labels?: string[];
}

export async function POST(request: NextRequest) {
  const tokenRecord = await requireAccessToken(request);
  if (!tokenRecord) {
    return NextResponse.json(asError("unauthorized", "valid bearer access token is required"), { status: 401 });
  }

  const subscription = await getActiveSubscription(tokenRecord.userId);
  if (!subscription) {
    return NextResponse.json(
      asError("subscription_required", "active subscription is required to register a TermX machine"),
      { status: 403 }
    );
  }

  const body = (await request.json()) as DeviceRegistrationBody;
  const deviceId = body.deviceId?.trim() ?? "";
  if (!deviceId) {
    return NextResponse.json(asError("invalid_device", "deviceId is required"), { status: 400 });
  }

  const machinePublicKey = body.machinePublicKey?.trim() ?? "";
  if (machinePublicKey) {
    try {
      const keyBytes = Buffer.from(machinePublicKey, "base64url");
      if (keyBytes.length !== 32) {
        return NextResponse.json(asError("invalid_public_key", "machinePublicKey must be a 32 byte Ed25519 key"), {
          status: 400,
        });
      }
    } catch {
      return NextResponse.json(asError("invalid_public_key", "machinePublicKey must use base64url encoding"), {
        status: 400,
      });
    }
  }

  const existing = await db.query.agents.findFirst({
    where: eq(agents.id, deviceId),
  });

  if (existing && existing.userId !== tokenRecord.userId) {
    return NextResponse.json(asError("machine_claimed", "machine is already registered to another user"), {
      status: 409,
    });
  }

  if (!existing) {
    const existingAgents = await db
      .select({ id: agents.id })
      .from(agents)
      .where(eq(agents.userId, tokenRecord.userId));
    if (existingAgents.length >= subscription.maxAgents) {
      return NextResponse.json(
        asError("agent_limit_exceeded", `maximum node limit reached (${subscription.maxAgents})`),
        { status: 403 }
      );
    }
  }

  const values = {
    id: deviceId,
    userId: tokenRecord.userId,
    tokenId: tokenRecord.id,
    publicKey: machinePublicKey || existing?.publicKey || null,
    name: body.displayName?.trim() || existing?.name || body.hostname?.trim() || deviceId,
    hostname: body.hostname?.trim() || existing?.hostname || "",
    osInfo: body.platform?.trim() || existing?.osInfo || "",
    labels: encodeAgentLabels({ labels: body.labels }),
    paired: existing?.paired ?? false,
    hubId: body.hubId?.trim() || existing?.hubId || null,
    lastSeen: new Date(),
  };

  await db
    .insert(agents)
    .values(values)
    .onConflictDoUpdate({
      target: agents.id,
      set: {
        userId: values.userId,
        tokenId: values.tokenId,
        publicKey: values.publicKey,
        name: values.name,
        hostname: values.hostname,
        osInfo: values.osInfo,
        labels: values.labels,
        hubId: values.hubId,
        lastSeen: values.lastSeen,
      },
    });

  await invalidateUserAgents(tokenRecord.userId);

  return NextResponse.json({
    device_id: deviceId,
    machine_id: deviceId,
    status: "active",
    user_id: tokenRecord.userId,
  });
}
