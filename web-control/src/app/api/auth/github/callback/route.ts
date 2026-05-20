import { NextRequest, NextResponse } from "next/server";
import crypto from "crypto";
import bcrypt from "bcryptjs";
import { and, eq, lt } from "drizzle-orm";
import { db } from "@/lib/db";
import { setAuthCookies, signAccessToken } from "@/lib/auth";
import { generateReferralCode } from "@/lib/referral.service";
import { referrals, refreshTokens, users } from "@/lib/schema";
import {
  buildAbsoluteAppUrl,
  buildGithubEntryPath,
  exchangeGithubCodeForToken,
  fetchGithubUser,
  getGithubOAuthCookieName,
  getSafePostLoginPath,
  GithubOAuthError,
  parseGithubOAuthCookieValue,
} from "@/lib/github-auth";

function buildCallbackErrorResponse(
  request: NextRequest,
  input: {
    entry?: string;
    from?: string;
    ref?: string;
    error: string;
  }
) {
  const response = NextResponse.redirect(
    buildAbsoluteAppUrl(request, buildGithubEntryPath(input))
  );
  response.cookies.delete(getGithubOAuthCookieName());
  return response;
}

function createOauthUsernameCandidate(login: string, name?: string | null): string {
  const source = login || name || "github-user";
  const normalized = source
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/-{2,}/g, "-")
    .replace(/^[-_]+|[-_]+$/g, "");

  const sliced = normalized.slice(0, 24);
  if (sliced.length >= 3) return sliced;
  return `gh-${(sliced || "user").padEnd(3, "x")}`.slice(0, 24);
}

async function ensureUniqueUsername(base: string): Promise<string> {
  const normalizedBase = createOauthUsernameCandidate(base);
  for (let attempt = 0; attempt < 100; attempt++) {
    const suffix = attempt === 0 ? "" : `-${attempt + 1}`;
    const candidate = `${normalizedBase.slice(0, 32 - suffix.length)}${suffix}`;
    const [existing] = await db
      .select({ id: users.id })
      .from(users)
      .where(eq(users.username, candidate))
      .limit(1);

    if (!existing) {
      return candidate;
    }
  }

  throw new GithubOAuthError("github_username_conflict", "生成用户名失败");
}

export async function GET(request: NextRequest) {
  const cookieValue = request.cookies.get(getGithubOAuthCookieName())?.value;
  const oauthState = parseGithubOAuthCookieValue(cookieValue);

  if (!oauthState) {
    return buildCallbackErrorResponse(request, {
      error: "github_state_invalid",
    });
  }

  const state = request.nextUrl.searchParams.get("state");
  const code = request.nextUrl.searchParams.get("code");
  const oauthError = request.nextUrl.searchParams.get("error");

  if (oauthError) {
    return buildCallbackErrorResponse(request, {
      entry: oauthState.entry,
      from: oauthState.from,
      ref: oauthState.ref,
      error: oauthError === "access_denied" ? "github_access_denied" : "github_oauth_failed",
    });
  }

  if (!state || state !== oauthState.state || !code) {
    return buildCallbackErrorResponse(request, {
      entry: oauthState.entry,
      from: oauthState.from,
      ref: oauthState.ref,
      error: "github_state_invalid",
    });
  }

  try {
    const githubToken = await exchangeGithubCodeForToken(code, request);
    const githubUser = await fetchGithubUser(githubToken);

    let [user] = await db
      .select()
      .from(users)
      .where(eq(users.githubId, githubUser.id))
      .limit(1);

    if (!user) {
      const [emailMatchedUser] = await db
        .select()
        .from(users)
        .where(eq(users.email, githubUser.email))
        .limit(1);

      if (emailMatchedUser?.githubId && emailMatchedUser.githubId !== githubUser.id) {
        throw new GithubOAuthError(
          "github_account_conflict",
          "该邮箱已绑定其他 GitHub 账号"
        );
      }

      if (emailMatchedUser) {
        [user] = await db
          .update(users)
          .set({
            githubId: githubUser.id,
            githubLogin: githubUser.login,
            githubAvatarUrl: githubUser.avatarUrl,
          })
          .where(eq(users.id, emailMatchedUser.id))
          .returning();
      } else {
        const username = await ensureUniqueUsername(
          createOauthUsernameCandidate(githubUser.login, githubUser.name)
        );

        let referrerId: string | null = null;
        if (oauthState.ref) {
          const [referrer] = await db
            .select({ id: users.id })
            .from(users)
            .where(eq(users.referralCode, oauthState.ref))
            .limit(1);
          if (referrer) {
            referrerId = referrer.id;
          }
        }

        const newReferralCode = generateReferralCode();

        [user] = await db
          .insert(users)
          .values({
            username,
            email: githubUser.email,
            passwordHash: null,
            githubId: githubUser.id,
            githubLogin: githubUser.login,
            githubAvatarUrl: githubUser.avatarUrl,
            role: "user",
            referralCode: newReferralCode,
            referredBy: referrerId,
          })
          .returning();

        if (referrerId) {
          await db.insert(referrals).values({
            referrerId,
            inviteeId: user.id,
            status: "pending",
          });
        }
      }
    } else {
      [user] = await db
        .update(users)
        .set({
          githubLogin: githubUser.login,
          githubAvatarUrl: githubUser.avatarUrl,
        })
        .where(eq(users.id, user.id))
        .returning();
    }

    const accessToken = await signAccessToken({
      userId: user.id,
      username: user.username,
      role: user.role,
    });

    const plainRefreshToken = crypto.randomBytes(32).toString("hex");
    const refreshTokenHash = await bcrypt.hash(plainRefreshToken, 10);
    const tokenLookup = crypto.createHash("sha256").update(plainRefreshToken).digest("hex");

    await db.insert(refreshTokens).values({
      userId: user.id,
      tokenHash: refreshTokenHash,
      tokenLookup,
      deviceName: request.headers.get("user-agent")?.slice(0, 100) || "github-oauth",
      expiresAt: new Date(Date.now() + 90 * 24 * 60 * 60 * 1000),
    });

    await db
      .delete(refreshTokens)
      .where(
        and(
          eq(refreshTokens.userId, user.id),
          lt(refreshTokens.expiresAt, new Date())
        )
      );

    await setAuthCookies(accessToken, plainRefreshToken);

    const needsLocalPasswordSetup = !user.passwordHash;
    const safePostLoginPath = getSafePostLoginPath(oauthState.from);
    const nextPath = needsLocalPasswordSetup
      ? `/dashboard/settings?setupLocalPassword=1&from=${encodeURIComponent(safePostLoginPath)}`
      : safePostLoginPath;

    const response = NextResponse.redirect(
      buildAbsoluteAppUrl(request, nextPath)
    );
    response.cookies.delete(getGithubOAuthCookieName());
    return response;
  } catch (error) {
    const errorCode =
      error instanceof GithubOAuthError ? error.code : "github_oauth_failed";
    console.error("GitHub callback error:", error);
    return buildCallbackErrorResponse(request, {
      entry: oauthState.entry,
      from: oauthState.from,
      ref: oauthState.ref,
      error: errorCode,
    });
  }
}
