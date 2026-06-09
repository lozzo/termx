import { NextResponse } from "next/server";
import { getActivePlans } from "@/lib/queries";

// GET /api/billing/plans — 获取活跃套餐列表
export async function GET() {
  const plans = await getActivePlans();
  return NextResponse.json(plans);
}
