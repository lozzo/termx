import { NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { getUserReferralStats } from "@/lib/queries";

export async function GET(request: Request) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "未登录" }, { status: 401 });
  }

  const stats = await getUserReferralStats(user.userId);
  return NextResponse.json(stats);
}
