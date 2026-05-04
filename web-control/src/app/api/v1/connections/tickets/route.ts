import crypto from "crypto";
import { NextRequest, NextResponse } from "next/server";
import { and, eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { agents, connectionTickets } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { getActiveSubscription } from "@/lib/subscription";
import {
  asError,
  defaultTerminalID,
  normalizeTerminalID,
  requireAccessToken,
  registeredTerminalIds,
} from "@/lib/termx-control";

interface CreateTicketBody {
  machine_id?: string;
  machineId?: string;
  terminal_id?: string;
  terminalId?: string;
  ttl_seconds?: number;
  ttlSeconds?: number;
}

export async function POST(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  const tokenRecord = user ? null : await requireAccessToken(request);
  const userId = user?.userId ?? tokenRecord?.userId ?? "";
  if (!userId) {
    return NextResponse.json(asError("unauthorized", "login or bearer auth is required"), { status: 401 });
  }

  const subscription = await getActiveSubscription(userId);
  if (!subscription) {
    return NextResponse.json(asError("subscription_required", "active subscription is required"), { status: 403 });
  }

  const body = (await request.json()) as CreateTicketBody;
  const machineId = (body.machine_id ?? body.machineId ?? "").trim();
  if (!machineId) {
    return NextResponse.json(asError("invalid_ticket", "machine_id is required"), { status: 400 });
  }

  const machine = await db.query.agents.findFirst({
    where: and(eq(agents.id, machineId), eq(agents.userId, userId)),
  });
  if (!machine) {
    return NextResponse.json(asError("machine_not_found", "machine not found"), { status: 404 });
  }
  if (!machine.online || !machine.hubId) {
    return NextResponse.json(asError("machine_offline", "machine is not online through a cloud hub"), {
      status: 503,
    });
  }

  const requestedTerminalID = normalizeTerminalID(body.terminal_id ?? body.terminalId);
  const terminalId = requestedTerminalID || defaultTerminalID(machine.labels);
  if (!terminalId) {
    return NextResponse.json(asError("terminal_required", "terminal_id is required for cloud connection"), {
      status: 400,
    });
  }
  const knownTerminals = registeredTerminalIds(machine.labels);
  if (knownTerminals.size > 0 && !knownTerminals.has(terminalId)) {
    return NextResponse.json(asError("terminal_not_found", "terminal is not registered on this machine"), {
      status: 404,
    });
  }

  const ttlSeconds = Math.max(30, Math.min(Number(body.ttl_seconds ?? body.ttlSeconds ?? 120), 600));
  const expiresAt = new Date(Date.now() + ttlSeconds * 1000);
  const allowRelay = Boolean(subscription.allowRelayTransfer);
  const id = `ct_${crypto.randomBytes(18).toString("base64url")}`;

  await db.insert(connectionTickets).values({
    id,
    userId,
    machineId,
    terminalId,
    path: "cloud",
    allowRelay,
    relayInUse: false,
    relayThrottled: false,
    expiresAt,
  });

  return NextResponse.json({
    id,
    path: "cloud",
    machine_id: machineId,
    terminal_id: terminalId,
    allow_relay: allowRelay,
    relay_in_use: false,
    relay_throttled: false,
    expires_at: expiresAt.toISOString(),
  });
}
