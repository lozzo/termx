import { NextResponse } from "next/server";
import { and, eq, isNull, gt } from "drizzle-orm";
import { db } from "@/lib/db";
import { connectionTickets } from "@/lib/schema";
import { asError, isAuthorizedHub } from "@/lib/termx-control";

interface TicketBody {
  ticket_id?: string;
  machine_id?: string;
  terminal_id?: string;
}

export async function POST(request: Request) {
  if (!isAuthorizedHub(request)) {
    return NextResponse.json(asError("hub_unauthorized", "valid hub secret is required"), { status: 401 });
  }

  const body = (await request.json()) as TicketBody;
  const ticketId = body.ticket_id?.trim() ?? "";
  const machineId = body.machine_id?.trim() ?? "";
  const terminalId = body.terminal_id?.trim() ?? "";
  if (!ticketId || !machineId || !terminalId) {
    return NextResponse.json(asError("invalid_ticket", "ticket_id, machine_id, and terminal_id are required"), {
      status: 400,
    });
  }

  const ticket = await db.query.connectionTickets.findFirst({
    where: and(
      eq(connectionTickets.id, ticketId),
      eq(connectionTickets.machineId, machineId),
      eq(connectionTickets.terminalId, terminalId),
      isNull(connectionTickets.consumedAt),
      gt(connectionTickets.expiresAt, new Date())
    ),
  });

  if (!ticket) {
    return NextResponse.json(asError("ticket_not_found", "connection ticket is invalid, expired, or consumed"), {
      status: 403,
    });
  }

  return NextResponse.json({
    ticket: {
      id: ticket.id,
      machine_id: ticket.machineId,
      terminal_id: ticket.terminalId,
      path: ticket.path,
      allow_relay: ticket.allowRelay,
      expires_at: ticket.expiresAt.toISOString(),
    },
  });
}
