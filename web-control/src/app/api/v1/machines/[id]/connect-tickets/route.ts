import { NextResponse } from "next/server";
import { asError, requireAccessToken } from "@/lib/termx-control";
import { issueConnectionTicket } from "@/lib/connection-tickets";

interface CreateTicketBody {
  terminal_id?: string;
  terminalId?: string;
  ttl_seconds?: number;
  ttlSeconds?: number;
}

export async function POST(
  request: Request,
  { params }: { params: Promise<{ id: string }> }
) {
  const tokenRecord = await requireAccessToken(request);
  if (!tokenRecord) {
    return NextResponse.json(asError("unauthorized", "valid bearer access token is required"), { status: 401 });
  }

  const { id } = await params;
  const body = (await request.json().catch(() => ({}))) as CreateTicketBody;
  const ticket = await issueConnectionTicket({
    userId: tokenRecord.userId,
    machineId: id,
    terminalId: body.terminal_id ?? body.terminalId ?? "",
    ttlSeconds: body.ttl_seconds ?? body.ttlSeconds,
  });
  if (!ticket) {
    return NextResponse.json(asError("machine_offline", "machine is not online through a cloud hub"), { status: 503 });
  }

  return NextResponse.json({
    connect_ticket: ticket.ticket,
    id: ticket.ticket,
    path: ticket.path,
    machine_id: ticket.machineId,
    terminal_id: ticket.terminalId,
    allow_relay: ticket.allowRelay,
    relay_in_use: false,
    relay_throttled: false,
    hub_http_url: ticket.hubHttpUrl,
    expires_at: ticket.expiresAt.toISOString(),
  });
}
