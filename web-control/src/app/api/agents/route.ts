import { NextRequest, NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { getUserAgents } from "@/lib/queries";

// GET /api/agents — 列出当前用户的 Agent
export async function GET(request: NextRequest) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const agents = await getUserAgents(user.userId);
  return NextResponse.json(agents);
}
