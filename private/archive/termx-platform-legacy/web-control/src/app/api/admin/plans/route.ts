import { NextResponse } from "next/server";
import { requireAdmin } from "@/lib/auth";
import { getAdminPlans } from "@/lib/queries";

export async function GET(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const result = await getAdminPlans();
  return NextResponse.json(result);
}
