import { NextResponse } from "next/server";
import { getAuthFromRequest } from "@/lib/auth";
import { getDashboardStats } from "@/lib/queries";

// GET /api/dashboard/stats — 返回仪表盘统计数据
export async function GET(request: Request) {
  const user = await getAuthFromRequest(request);
  if (!user) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  const stats = await getDashboardStats(user.userId);
  return NextResponse.json(stats);
}
