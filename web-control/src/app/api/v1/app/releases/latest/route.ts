import { NextResponse } from "next/server";
import { db } from "@/lib/db";
import { appReleases } from "@/lib/schema";
import { and, eq, desc } from "drizzle-orm";
import { buildRequestUrl } from "@/lib/url";
import { cacheKeys, cacheManager, cacheTtl } from "@/lib/cache-system";

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const platform = searchParams.get("platform") || "android";
  const type = searchParams.get("type") || "apk";
  const redirect = searchParams.get("redirect") === "true";

  const latest = await cacheManager.getOrLoad({
    key: cacheKeys.appLatestRelease(platform, type),
    ttlMs: cacheTtl.publicHot,
    negativeTtlMs: cacheTtl.negativeShort,
    loader: async () => {
      const result = await db
        .select()
        .from(appReleases)
        .where(
          and(
            eq(appReleases.platform, platform),
            eq(appReleases.type, type)
          )
        )
        .orderBy(desc(appReleases.versionCode), desc(appReleases.id))
        .limit(1);

      if (result.length === 0) {
        return null;
      }

      const release = result[0];
      return {
        version: release.version,
        versionCode: release.versionCode,
        fileName: release.fileName,
        fileSize: Number(release.fileSize),
        fileHash: release.fileHash,
        changelog: release.changelog,
        forceUpdate: release.forceUpdate,
        downloadUrl: release.downloadUrl,
        mirrors: JSON.parse(release.mirrors || "[]"),
      };
    },
  });

  if (!latest) {
    if (redirect) {
      return NextResponse.redirect(buildRequestUrl(request, "/"));
    }
    return NextResponse.json({ update: false });
  }

  const cacheHeaders = {
    "Cache-Control": "public, max-age=300, s-maxage=300, stale-while-revalidate=60",
  };

  if (redirect && latest.downloadUrl) {
    return NextResponse.redirect(latest.downloadUrl, {
      status: 302,
      headers: cacheHeaders,
    });
  }

  return NextResponse.json(
    {
      update: true,
      version: latest.version,
      versionCode: latest.versionCode,
      fileName: latest.fileName,
      fileSize: latest.fileSize,
      fileHash: latest.fileHash,
      changelog: latest.changelog,
      forceUpdate: latest.forceUpdate,
      downloadUrl: latest.downloadUrl,
      mirrors: latest.mirrors,
    },
    { headers: cacheHeaders }
  );
}
