import { NextResponse } from "next/server";
import { requireAdmin } from "@/lib/auth";
import { getAdminAppReleases } from "@/lib/queries";
import { db } from "@/lib/db";
import { appReleases } from "@/lib/schema";
import { invalidateAdminAppReleases, invalidateAppLatestRelease } from "@/lib/cache-system";
import { and, eq } from "drizzle-orm";

export async function GET(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const result = await getAdminAppReleases();
  return NextResponse.json(result);
}

export async function POST(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const body = await request.json();
  const { platform, type, version, versionCode, fileName, fileSize, fileHash, downloadUrl, mirrors, changelog, forceUpdate } = body;

  if (!type || !version || !versionCode || !fileName) {
    return NextResponse.json({ error: "type, version, versionCode, and fileName are required" }, { status: 400 });
  }

  // 删除同 (platform, type, versionCode) 的旧记录，保证唯一
  await db
    .delete(appReleases)
    .where(
      and(
        eq(appReleases.platform, platform || "android"),
        eq(appReleases.type, type),
        eq(appReleases.versionCode, versionCode)
      )
    );

  const [record] = await db
    .insert(appReleases)
    .values({
      platform: platform || "android",
      type,
      version,
      versionCode,
      fileName,
      fileSize: String(fileSize || 0),
      fileHash: fileHash || "",
      downloadUrl: downloadUrl || "",
      mirrors: mirrors ? JSON.stringify(mirrors) : "[]",
      changelog: changelog || "",
      forceUpdate: forceUpdate === true,
    })
    .returning();

  await Promise.all([
    invalidateAppLatestRelease(record.platform, record.type),
    invalidateAdminAppReleases(),
  ]);

  return NextResponse.json(record, { status: 201 });
}
