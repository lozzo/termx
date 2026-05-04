import crypto from "crypto";
import { NextResponse } from "next/server";
import { eq } from "drizzle-orm";
import { db } from "@/lib/db";
import { agents } from "@/lib/schema";
import { asError, isAuthorizedHub } from "@/lib/termx-control";

interface VerifyRegistrationBody {
  machine_id?: string;
  agent_id?: string;
  signature?: {
    algorithm?: string;
    nonce?: string;
    timestamp?: number;
    value?: string;
  };
}

export async function POST(request: Request) {
  if (!isAuthorizedHub(request)) {
    return NextResponse.json(asError("hub_unauthorized", "valid hub secret is required"), { status: 401 });
  }

  const body = (await request.json()) as VerifyRegistrationBody;
  const machineId = body.machine_id?.trim() ?? "";
  const agentId = body.agent_id?.trim() ?? "";
  const signature = body.signature;
  if (
    !machineId ||
    !agentId ||
    signature?.algorithm?.trim().toLowerCase() !== "ed25519" ||
    !signature.nonce?.trim() ||
    !signature.timestamp ||
    !signature.value?.trim()
  ) {
    return NextResponse.json(asError("invalid_agent_registration", "machine_id, agent_id, and signature are required"), {
      status: 400,
    });
  }

  const machine = await db.query.agents.findFirst({
    where: eq(agents.id, machineId),
  });
  if (!machine?.publicKey || !machine.userId) {
    return NextResponse.json(asError("machine_not_found", "registered machine public key was not found"), {
      status: 403,
    });
  }

  const nowSeconds = Math.floor(Date.now() / 1000);
  if (Math.abs(nowSeconds - signature.timestamp) > 5 * 60) {
    return NextResponse.json(asError("signature_stale", "agent registration signature timestamp is outside replay window"), {
      status: 403,
    });
  }

  let publicKey: Buffer;
  let signatureBytes: Buffer;
  try {
    publicKey = Buffer.from(machine.publicKey, "base64url");
    signatureBytes = Buffer.from(signature.value, "base64");
  } catch {
    return NextResponse.json(asError("invalid_signature", "signature or public key encoding is invalid"), {
      status: 403,
    });
  }
  if (publicKey.length !== 32 || signatureBytes.length !== 64) {
    return NextResponse.json(asError("invalid_signature", "signature or public key size is invalid"), {
      status: 403,
    });
  }

  const message = canonicalAgentRegistrationMessage({
    machineId,
    agentId,
    nonce: signature.nonce,
    timestamp: signature.timestamp,
  });
  const valid = crypto.verify(null, message, ed25519PublicKeyFromRaw(publicKey), signatureBytes);
  if (!valid) {
    return NextResponse.json(asError("invalid_signature", "agent registration signature verification failed"), {
      status: 403,
    });
  }

  await db
    .update(agents)
    .set({ online: true, lastSeen: new Date() })
    .where(eq(agents.id, machineId));

  return NextResponse.json({ valid: true });
}

function ed25519PublicKeyFromRaw(raw: Buffer): crypto.KeyObject {
  const ed25519SpkiPrefix = Buffer.from("302a300506032b6570032100", "hex");
  return crypto.createPublicKey({
    key: Buffer.concat([ed25519SpkiPrefix, raw]),
    format: "der",
    type: "spki",
  });
}

function canonicalAgentRegistrationMessage(input: {
  machineId: string;
  agentId: string;
  nonce: string;
  timestamp: number;
}): Buffer {
  const machineHash = crypto.createHash("sha256").update(input.machineId.trim()).digest("hex");
  const agentHash = crypto.createHash("sha256").update(input.agentId.trim()).digest("hex");
  return Buffer.from(
    [
      "termx-agent-registration-v1:",
      `sha256(machine_id):${machineHash}`,
      `sha256(agent_id):${agentHash}`,
      `nonce:${input.nonce.trim()}`,
      `timestamp:${Math.trunc(input.timestamp)}`,
    ].join("\n")
  );
}
