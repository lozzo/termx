import { NextRequest, NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agents } from "@/lib/schema";
import { getAuthFromRequest } from "@/lib/auth";
import { getActiveSubscription } from "@/lib/subscription";
import { eq, and } from "drizzle-orm";
import { invalidateUserAgents } from "@/lib/cache-system";

// POST /api/agents/:id/pair — 标记 agent 已配对
export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> }
) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const subscription = await getActiveSubscription(user.userId);
  if (!subscription) {
    return NextResponse.json(
      { error: "subscription_required", message: "需要有效订阅才能配对节点" },
      { status: 403 }
    );
  }

  const { id } = await params;

  const agent = await db.query.agents.findFirst({
    where: and(eq(agents.id, id), eq(agents.userId, user.userId)),
  });

  if (!agent) {
    return NextResponse.json({ error: "agent not found" }, { status: 404 });
  }

  await db
    .update(agents)
    .set({ paired: true })
    .where(eq(agents.id, id));

  await invalidateUserAgents(user.userId);

  return NextResponse.json({ id, paired: true });
}
