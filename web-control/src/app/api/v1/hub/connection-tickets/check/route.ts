import { NextResponse } from "next/server";
import { connectionTicketResponse, verifyConnectionTicket } from "@/lib/connection-tickets";
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
  const ticket = await verifyConnectionTicket({
    ticketId: body.ticket_id?.trim() ?? "",
    machineId: body.machine_id?.trim() ?? "",
    terminalId: body.terminal_id?.trim() ?? "",
  }).catch(() => null);
  if (!ticket) {
    return NextResponse.json(asError("ticket_not_found", "connection ticket is invalid or expired"), { status: 403 });
  }

  return NextResponse.json(connectionTicketResponse(ticket));
}
