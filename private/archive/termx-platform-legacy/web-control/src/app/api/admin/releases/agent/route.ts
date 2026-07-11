import { NextResponse } from "next/server";
import { requireAdmin } from "@/lib/auth";
import { getAdminAgentReleases } from "@/lib/queries";
import { db } from "@/lib/db";
import { agentReleases } from "@/lib/schema";
import { invalidateAdminAgentReleases, invalidateAgentLatestRelease } from "@/lib/cache-system";

export async function GET(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const result = await getAdminAgentReleases();
  return NextResponse.json(result);
}

export async function POST(request: Request) {
  const admin = await requireAdmin(request);
  if (!admin) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  const body = await request.json();
  const { version, os, arch, downloadUrl, mirrors, sha256, changelog, forceUpdate } = body;

  if (!version || !downloadUrl) {
    return NextResponse.json({ error: "version and downloadUrl are required" }, { status: 400 });
  }

  const [record] = await db
    .insert(agentReleases)
    .values({
      version,
      os: os || "linux",
      arch: arch || "amd64",
      downloadUrl,
      mirrors: Array.isArray(mirrors) ? JSON.stringify(mirrors) : mirrors || "[]",
      sha256: sha256 || "",
      changelog: changelog || "",
      forceUpdate: forceUpdate === true,
    })
    .returning();

  await Promise.all([
    invalidateAgentLatestRelease(record.os, record.arch),
    invalidateAdminAgentReleases(),
  ]);

  return NextResponse.json(record, { status: 201 });
}
