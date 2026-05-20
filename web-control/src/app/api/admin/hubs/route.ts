import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { hubs } from "@/lib/schema";
import { requireAdmin } from "@/lib/auth";
import { getAdminHubs } from "@/lib/queries";
import { invalidateAdminHubs } from "@/lib/cache-system";

export async function GET(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const result = await getAdminHubs();
  return NextResponse.json(result);
}

export async function POST(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const body = await request.json();
  const { name, region, httpUrl, grpcUrl } = body;

  if (!name) {
    return NextResponse.json({ error: "name required" }, { status: 400 });
  }

  const [hub] = await db
    .insert(hubs)
    .values({
      name,
      region: region || "",
      httpUrl: httpUrl || "",
      grpcUrl: grpcUrl || "",
    })
    .returning();

  await invalidateAdminHubs();
  return NextResponse.json(hub, { status: 201 });
}
