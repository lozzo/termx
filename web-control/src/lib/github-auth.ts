import crypto from "crypto";
import {
  getGithubClientId,
  getGithubClientSecret,
} from "./config";
import { buildRequestUrl, sanitizeRedirectPath } from "./url";

const GITHUB_AUTHORIZE_URL = "https://github.com/login/oauth/authorize";
const GITHUB_TOKEN_URL = "https://github.com/login/oauth/access_token";
const GITHUB_API_BASE = "https://api.github.com";
const GITHUB_OAUTH_COOKIE = "termx_github_oauth";
const GITHUB_OAUTH_TTL_MS = 10 * 60 * 1000;

type AuthEntry = "login" | "register";

interface GithubOAuthCookiePayload {
  state: string;
  entry: AuthEntry;
  from?: string;
  ref?: string;
  expiresAt: number;
}

interface GithubEmail {
  email: string;
  primary?: boolean;
  verified?: boolean;
}

interface GithubUserProfileResponse {
  id: number;
  login: string;
  name: string | null;
  email: string | null;
  avatar_url: string | null;
}

export interface GithubResolvedUser {
  id: string;
  login: string;
  name: string | null;
  email: string;
  avatarUrl: string | null;
}

export class GithubOAuthError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "GithubOAuthError";
  }
}

export function getGithubOAuthCookieName(): string {
  return GITHUB_OAUTH_COOKIE;
}

export function createGithubOAuthState(): string {
  return crypto.randomBytes(24).toString("hex");
}

export function getGithubOAuthCookieMaxAgeSeconds(): number {
  return Math.floor(GITHUB_OAUTH_TTL_MS / 1000);
}

export function buildGithubOAuthCookieValue(input: {
  state: string;
  entry?: string | null;
  from?: string | null;
  ref?: string | null;
}): string {
  const payload: GithubOAuthCookiePayload = {
    state: input.state,
    entry: sanitizeEntry(input.entry),
    from: sanitizeRedirectPath(input.from),
    ref: sanitizeReferralCode(input.ref),
    expiresAt: Date.now() + GITHUB_OAUTH_TTL_MS,
  };

  return Buffer.from(JSON.stringify(payload)).toString("base64url");
}

export function parseGithubOAuthCookieValue(
  rawValue?: string
): GithubOAuthCookiePayload | null {
  if (!rawValue) return null;

  try {
    const parsed = JSON.parse(
      Buffer.from(rawValue, "base64url").toString("utf8")
    ) as Partial<GithubOAuthCookiePayload>;

    if (
      typeof parsed.state !== "string" ||
      typeof parsed.expiresAt !== "number" ||
      parsed.expiresAt < Date.now()
    ) {
      return null;
    }

    return {
      state: parsed.state,
      entry: sanitizeEntry(parsed.entry),
      from: sanitizeRedirectPath(parsed.from),
      ref: sanitizeReferralCode(parsed.ref),
      expiresAt: parsed.expiresAt,
    };
  } catch {
    return null;
  }
}

export function buildGithubAuthorizeUrl(
  request: Request,
  state: string
): string {
  const url = new URL(GITHUB_AUTHORIZE_URL);
  url.searchParams.set("client_id", getGithubClientId());
  url.searchParams.set("redirect_uri", getGithubRedirectUri(request));
  url.searchParams.set("scope", "read:user user:email");
  url.searchParams.set("state", state);
  return url.toString();
}

export function getGithubRedirectUri(request: Request): string {
  return buildRequestUrl(request, "/api/auth/github/callback").toString();
}

export function buildAbsoluteAppUrl(
  request: Request,
  path: string
): URL {
  return buildRequestUrl(request, path);
}

export function getSafePostLoginPath(path?: string): string {
  return sanitizeRedirectPath(path) || "/dashboard";
}

export function buildGithubEntryPath(input: {
  entry?: string | null;
  from?: string | null;
  ref?: string | null;
  error?: string;
}): string {
  const entry = sanitizeEntry(input.entry);
  const basePath = entry === "register" ? "/register" : "/login";
  const params = new URLSearchParams();
  const from = sanitizeRedirectPath(input.from);
  const ref = sanitizeReferralCode(input.ref);

  if (from) params.set("from", from);
  if (ref) params.set("ref", ref);
  if (input.error) params.set("error", input.error);

  const query = params.toString();
  return query ? `${basePath}?${query}` : basePath;
}

export function sanitizeReferralCode(value?: string | null): string | undefined {
  const trimmed = value?.trim();
  if (!trimmed) return undefined;
  return trimmed.slice(0, 64);
}

function sanitizeEntry(value?: string | null): AuthEntry {
  return value === "register" ? "register" : "login";
}

export async function exchangeGithubCodeForToken(
  code: string,
  request: Request
): Promise<string> {
  const res = await fetch(GITHUB_TOKEN_URL, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      "User-Agent": "termx-web-control",
    },
    body: JSON.stringify({
      client_id: getGithubClientId(),
      client_secret: getGithubClientSecret(),
      code,
      redirect_uri: getGithubRedirectUri(request),
    }),
    cache: "no-store",
  });

  if (!res.ok) {
    throw new GithubOAuthError("github_token_exchange_failed", "GitHub token 交换失败");
  }

  const data = (await res.json()) as {
    access_token?: string;
    error?: string;
    error_description?: string;
  };

  if (!data.access_token) {
    throw new GithubOAuthError(
      "github_token_exchange_failed",
      data.error_description || data.error || "GitHub 未返回 access token"
    );
  }

  return data.access_token;
}

export async function fetchGithubUser(
  accessToken: string
): Promise<GithubResolvedUser> {
  const headers = {
    Accept: "application/vnd.github+json",
    Authorization: `Bearer ${accessToken}`,
    "User-Agent": "termx-web-control",
  };

  const profileRes = await fetch(`${GITHUB_API_BASE}/user`, {
    headers,
    cache: "no-store",
  });

  if (!profileRes.ok) {
    throw new GithubOAuthError("github_profile_fetch_failed", "获取 GitHub 用户信息失败");
  }

  const profile = (await profileRes.json()) as GithubUserProfileResponse;

  const emailsRes = await fetch(`${GITHUB_API_BASE}/user/emails`, {
    headers,
    cache: "no-store",
  });

  if (!emailsRes.ok) {
    throw new GithubOAuthError("github_email_fetch_failed", "获取 GitHub 邮箱失败");
  }

  const emails = (await emailsRes.json()) as GithubEmail[];
  const verifiedEmail =
    emails.find((item) => item.primary && item.verified)?.email ||
    emails.find((item) => item.verified)?.email ||
    profile.email;

  if (!verifiedEmail) {
    throw new GithubOAuthError(
      "github_email_unavailable",
      "GitHub 账户缺少可用的已验证邮箱"
    );
  }

  return {
    id: String(profile.id),
    login: profile.login,
    name: profile.name,
    email: verifiedEmail,
    avatarUrl: profile.avatar_url,
  };
}
