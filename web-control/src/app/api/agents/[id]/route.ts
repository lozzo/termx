import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents, hubs } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { getHubSecret } from "@/lib/config";
import { eq, and } from "drizzle-orm";
import { invalidateUserAgents } from "@/lib/cache-system";

// PUT /api/agents/:id — 重命名
export async function PUT(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id } = await params;
  const body = await request.json();
  const name = body.name;
  if (!name || typeof name !== "string") {
    return NextResponse.json({ error: "name required" }, { status: 400 });
  }

  const [agent] = await db
    .select()
    .from(agents)
    .where(and(eq(agents.id, id), eq(agents.userId, user.userId)))
    .limit(1);

  if (!agent) {
    return NextResponse.json({ error: "agent not found" }, { status: 404 });
  }

  await db.update(agents).set({ name }).where(eq(agents.id, id));
  await invalidateUserAgents(user.userId);
  return NextResponse.json({ id, name });
}

// DELETE /api/agents/:id — 删除（等效吊销 token）
export async function DELETE(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const { id } = await params;
  const [agent] = await db
    .select()
    .from(agents)
    .where(and(eq(agents.id, id), eq(agents.userId, user.userId)))
    .limit(1);

  if (!agent) {
    return NextResponse.json({ error: "agent not found" }, { status: 404 });
  }

  // 如果 agent 在线且有 hubId，通知 Hub 踢掉它
  if (agent.online && agent.hubId) {
    try {
      const [hub] = await db
        .select({ httpUrl: hubs.httpUrl })
        .from(hubs)
        .where(eq(hubs.id, agent.hubId))
        .limit(1);

      if (hub?.httpUrl) {
        await fetch(`${hub.httpUrl}/api/internal/kick`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Hub-Secret": getHubSecret(),
          },
          body: JSON.stringify({ agent_id: id }),
          signal: AbortSignal.timeout(5000),
        });
      }
    } catch (e) {
      console.error(`[DELETE /api/agents/${id}] failed to notify hub:`, e);
    }
  }

  await db.delete(agents).where(eq(agents.id, id));
  await invalidateUserAgents(user.userId);
  return new NextResponse(null, { status: 204 });
}
