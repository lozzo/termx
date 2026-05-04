import crypto from "crypto";
import { and, eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { getHubSecret } from "@/lib/config";
import { agents } from "@/lib/schema";

const TICKET_VERSION = 1;
const MIN_TTL_SECONDS = 30;
const MAX_TTL_SECONDS = 600;
const DEFAULT_TTL_SECONDS = 120;

export interface ConnectionTicketPayload {
  v: number;
  id: string;
  user_id: string;
  machine_id: string;
  terminal_id: string;
  path: "cloud";
  allow_relay: boolean;
  exp: number;
  iat: number;
}

export interface IssuedConnectionTicket {
  id: string;
  machineId: string;
  terminalId: string;
  path: "cloud";
  allowRelay: boolean;
  expiresAt: Date;
  hubHttpUrl: string;
  ticket: string;
}

export async function issueConnectionTicket(input: {
  userId: string;
  machineId: string;
  terminalId?: string;
  ttlSeconds?: number;
}): Promise<IssuedConnectionTicket | null> {
  const machineId = input.machineId.trim();
  if (!machineId) return null;
  const machine = await db.query.agents.findFirst({
    where: and(eq(agents.id, machineId), eq(agents.userId, input.userId)),
    with: {
      hub: {
        columns: {
          httpUrl: true,
          status: true,
        },
      },
    },
  });
  if (!machine || !machine.online || !machine.hubId || !machine.hub?.httpUrl) {
    return null;
  }

  const now = Math.floor(Date.now() / 1000);
  const ttl = clampTTL(input.ttlSeconds);
  const expiresAt = new Date((now + ttl) * 1000);
  const payload: ConnectionTicketPayload = {
    v: TICKET_VERSION,
    id: `ct_${crypto.randomBytes(18).toString("base64url")}`,
    user_id: input.userId,
    machine_id: machineId,
    terminal_id: input.terminalId?.trim() ?? "",
    path: "cloud",
    allow_relay: false,
    iat: now,
    exp: now + ttl,
  };
  return {
    id: payload.id,
    machineId: payload.machine_id,
    terminalId: payload.terminal_id,
    path: payload.path,
    allowRelay: payload.allow_relay,
    expiresAt,
    hubHttpUrl: machine.hub.httpUrl,
    ticket: encodeConnectionTicket(payload),
  };
}

export async function verifyConnectionTicket(input: {
  ticketId: string;
  machineId: string;
  terminalId?: string;
}): Promise<IssuedConnectionTicket | null> {
  const payload = decodeConnectionTicket(input.ticketId);
  const machineId = input.machineId.trim();
  const terminalId = input.terminalId?.trim() ?? "";
  if (
    payload.machine_id !== machineId ||
    payload.terminal_id !== terminalId ||
    payload.path !== "cloud" ||
    payload.exp <= Math.floor(Date.now() / 1000)
  ) {
    return null;
  }
  const machine = await db.query.agents.findFirst({
    where: and(eq(agents.id, payload.machine_id), eq(agents.userId, payload.user_id)),
    with: {
      hub: {
        columns: {
          httpUrl: true,
          status: true,
        },
      },
    },
  });
  if (!machine || !machine.online || !machine.hubId || !machine.hub?.httpUrl) {
    return null;
  }
  return {
    id: payload.id,
    machineId: payload.machine_id,
    terminalId: payload.terminal_id,
    path: payload.path,
    allowRelay: payload.allow_relay,
    expiresAt: new Date(payload.exp * 1000),
    hubHttpUrl: machine.hub.httpUrl,
    ticket: input.ticketId,
  };
}

export function connectionTicketResponse(ticket: IssuedConnectionTicket) {
  return {
    ticket: {
      id: ticket.ticket,
      machine_id: ticket.machineId,
      terminal_id: ticket.terminalId,
      path: ticket.path,
      allow_relay: ticket.allowRelay,
      expires_at: ticket.expiresAt.toISOString(),
    },
  };
}

function encodeConnectionTicket(payload: ConnectionTicketPayload): string {
  const body = Buffer.from(JSON.stringify(payload)).toString("base64url");
  const signature = crypto
    .createHmac("sha256", getHubSecret())
    .update(body)
    .digest("base64url");
  return `${body}.${signature}`;
}

function decodeConnectionTicket(value: string): ConnectionTicketPayload {
  const [body, signature, extra] = value.trim().split(".");
  if (!body || !signature || extra !== undefined) {
    throw new Error("connection ticket format is invalid");
  }
  const expected = crypto
    .createHmac("sha256", getHubSecret())
    .update(body)
    .digest("base64url");
  if (!timingSafeEqual(signature, expected)) {
    throw new Error("connection ticket signature is invalid");
  }
  const decoded = JSON.parse(Buffer.from(body, "base64url").toString("utf8")) as Partial<ConnectionTicketPayload>;
  if (
    decoded.v !== TICKET_VERSION ||
    typeof decoded.id !== "string" ||
    typeof decoded.user_id !== "string" ||
    typeof decoded.machine_id !== "string" ||
    typeof decoded.terminal_id !== "string" ||
    decoded.path !== "cloud" ||
    typeof decoded.allow_relay !== "boolean" ||
    typeof decoded.exp !== "number" ||
    typeof decoded.iat !== "number"
  ) {
    throw new Error("connection ticket payload is invalid");
  }
  return decoded as ConnectionTicketPayload;
}

function timingSafeEqual(left: string, right: string): boolean {
  const a = Buffer.from(left);
  const b = Buffer.from(right);
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}

function clampTTL(value: number | undefined): number {
  if (!Number.isFinite(value)) return DEFAULT_TTL_SECONDS;
  return Math.max(MIN_TTL_SECONDS, Math.min(Math.trunc(value ?? DEFAULT_TTL_SECONDS), MAX_TTL_SECONDS));
}
