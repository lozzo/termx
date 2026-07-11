import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { agentReleases } from "@/lib/schema";
import { and, eq, desc } from "drizzle-orm";
import { cacheKeys, cacheManager, cacheTtl } from "@/lib/cache-system";
import { isNewerVersion } from "@/lib/semver";

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const currentVersion = searchParams.get("version");
  const os = searchParams.get("os") || "linux";
  const arch = searchParams.get("arch") || "amd64";

  if (!currentVersion) {
    return NextResponse.json({ error: "version is required" }, { status: 400 });
  }

  const latest = await cacheManager.getOrLoad({
    key: cacheKeys.agentLatestRelease(os, arch),
    ttlMs: cacheTtl.publicHot,
    negativeTtlMs: cacheTtl.negativeShort,
    loader: async () => {
      const result = await db
        .select()
        .from(agentReleases)
        .where(
          and(
            eq(agentReleases.os, os),
            eq(agentReleases.arch, arch),
            eq(agentReleases.active, true)
          )
        )
        .orderBy(desc(agentReleases.createdAt))
        .limit(1);

      if (result.length === 0) {
        return null;
      }

      const release = result[0];
      let mirrors: string[] = [];
      try {
        mirrors = JSON.parse(release.mirrors);
      } catch {}

      return {
        version: release.version,
        downloadUrl: release.downloadUrl,
        mirrors,
        sha256: release.sha256,
        forceUpdate: release.forceUpdate,
        changelog: release.changelog,
      };
    },
  });

  if (!latest) {
    return NextResponse.json({ update: false });
  }

  if (!isNewerVersion(latest.version, currentVersion)) {
    return NextResponse.json({ update: false });
  }

  return NextResponse.json({
    update: true,
    version: latest.version,
    downloadUrl: latest.downloadUrl,
    mirrors: latest.mirrors,
    sha256: latest.sha256,
    forceUpdate: latest.forceUpdate,
    changelog: latest.changelog,
  });
}
