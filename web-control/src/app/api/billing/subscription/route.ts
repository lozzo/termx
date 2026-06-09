import { NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { getUserSubscription } from "@/lib/queries";

// GET /api/billing/subscription — 获取当前用户订阅
export async function GET(request: Request) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const sub = await getUserSubscription(user.userId);
  return NextResponse.json(sub);
}
