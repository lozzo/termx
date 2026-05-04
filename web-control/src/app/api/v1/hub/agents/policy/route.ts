import { NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { agents } from "@/lib/schema";
import { asError, isAuthorizedHub } from "@/lib/termx-control";

interface AgentPolicyBody {
  machine_id?: string;
  agent_id?: string;
}

export async function POST(request: Request) {
  if (!isAuthorizedHub(request)) {
    return NextResponse.json(asError("hub_unauthorized", "valid hub secret is required"), { status: 401 });
  }

  const body = (await request.json()) as AgentPolicyBody;
  const machineId = body.machine_id?.trim() ?? "";
  const agentId = body.agent_id?.trim() ?? "";
  if (!machineId || !agentId) {
    return NextResponse.json(asError("invalid_policy_request", "machine_id and agent_id are required"), {
      status: 400,
    });
  }

  const machine = await db.query.agents.findFirst({
    where: eq(agents.id, machineId),
  });
  if (!machine) {
    return NextResponse.json(asError("machine_not_found", "machine was not found"), { status: 404 });
  }

  return NextResponse.json({
    policy: {
      machine_id: machineId,
      agent_id: agentId,
      force_offline: machine.pendingKick,
      reason: machine.pendingKick ? "force offline requested from Web Control" : "",
    },
  });
}
