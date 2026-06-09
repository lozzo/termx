import { NextResponse } from "next/server";
import { requireAdmin } from "@/lib/auth";
import { getPresignedUploadUrl } from "@/lib/s3";

export async function POST(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const { key, contentType } = await request.json();
  if (!key) {
    return NextResponse.json({ error: "key is required" }, { status: 400 });
  }

  const url = await getPresignedUploadUrl(key, contentType || "application/octet-stream");
  return NextResponse.json({ url });
}
