import { NextRequest, NextResponse } from "next/server";
import { isGithubOAuthConfigured } from "@/lib/config";
import {
  buildGithubAuthorizeUrl,
  buildGithubEntryPath,
  buildGithubOAuthCookieValue,
  createGithubOAuthState,
  getGithubOAuthCookieMaxAgeSeconds,
  getGithubOAuthCookieName,
} from "@/lib/github-auth";
import { buildRequestUrl } from "@/lib/url";

export async function GET(request: NextRequest) {
  const entry = request.nextUrl.searchParams.get("entry");
  const from = request.nextUrl.searchParams.get("from");
  const ref = request.nextUrl.searchParams.get("ref");

  if (!isGithubOAuthConfigured()) {
    return NextResponse.redirect(
      buildRequestUrl(
        request,
        buildGithubEntryPath({
          entry,
          from,
          ref,
          error: "github_not_configured",
        })
      )
    );
  }

  const state = createGithubOAuthState();
  const response = NextResponse.redirect(buildGithubAuthorizeUrl(request, state));

  response.cookies.set(getGithubOAuthCookieName(), buildGithubOAuthCookieValue({
    state,
    entry,
    from,
    ref,
  }), {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: getGithubOAuthCookieMaxAgeSeconds(),
  });

  return response;
}
