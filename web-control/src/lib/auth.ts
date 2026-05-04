import { SignJWT, jwtVerify } from "jose";
import { cookies } from "next/headers";
import { getAppUrl, getJwtSecret } from "./config";
import { db } from "./db";
import { refreshTokens, users } from "./schema";
import { eq, and, gt } from "drizzle-orm";
import bcrypt from "bcryptjs";
import crypto from "crypto";

const ACCESS_TOKEN_EXPIRY = "1h";
const REFRESH_TOKEN_COOKIE = "termx_refresh_token";
const ACCESS_TOKEN_COOKIE = "termx_access_token";

// Refresh token 配置
const REFRESH_TOKEN_TOTAL_MS = 90 * 24 * 60 * 60 * 1000; // 90 天
const REFRESH_TOKEN_RENEW_THRESHOLD_MS = REFRESH_TOKEN_TOTAL_MS / 2; // 45 天

export interface JWTPayload {
  userId: string;
  username: string;
  role: string;
}

function parseBooleanEnv(value: string | undefined): boolean | undefined {
  if (!value) return undefined;
  const normalized = value.trim().toLowerCase();
  if (["1", "true", "yes", "on"].includes(normalized)) return true;
  if (["0", "false", "no", "off"].includes(normalized)) return false;
  return undefined;
}

function shouldUseSecureAuthCookies(): boolean {
  const explicit =
    parseBooleanEnv(process.env.AUTH_COOKIE_SECURE) ??
    parseBooleanEnv(process.env.COOKIE_SECURE);
  if (explicit !== undefined) return explicit;

  const appUrl = getAppUrl();
  if (appUrl) {
    return appUrl.startsWith("https://");
  }

  return process.env.NODE_ENV === "production";
}

export async function signAccessToken(payload: JWTPayload): Promise<string> {
  return new SignJWT({ ...payload })
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime(ACCESS_TOKEN_EXPIRY)
    .sign(getJwtSecret());
}

export async function verifyAccessToken(
  token: string
): Promise<JWTPayload | null> {
  try {
    const { payload } = await jwtVerify(token, getJwtSecret());
    return {
      userId: payload.userId as string,
      username: payload.username as string,
      role: payload.role as string,
    };
  } catch {
    return null;
  }
}

export async function setAuthCookies(
  accessToken: string,
  refreshToken?: string
) {
  const cookieStore = await cookies();

  cookieStore.set(ACCESS_TOKEN_COOKIE, accessToken, {
    httpOnly: true,
    secure: shouldUseSecureAuthCookies(),
    sameSite: "lax",
    path: "/",
    maxAge: 60 * 60, // 1 hour
  });

  if (refreshToken) {
    cookieStore.set(REFRESH_TOKEN_COOKIE, refreshToken, {
      httpOnly: true,
      secure: shouldUseSecureAuthCookies(),
      sameSite: "lax",
      path: "/",
      maxAge: 60 * 60 * 24 * 90, // 90 days
    });
  }
}

export async function clearAuthCookies() {
  const cookieStore = await cookies();
  cookieStore.delete(ACCESS_TOKEN_COOKIE);
  cookieStore.delete(REFRESH_TOKEN_COOKIE);
}

export async function getAccessTokenFromCookie(): Promise<string | undefined> {
  const cookieStore = await cookies();
  return cookieStore.get(ACCESS_TOKEN_COOKIE)?.value;
}

export async function getRefreshTokenFromCookie(): Promise<string | undefined> {
  const cookieStore = await cookies();
  return cookieStore.get(REFRESH_TOKEN_COOKIE)?.value;
}

export async function getCurrentUser(): Promise<JWTPayload | null> {
  const token = await getAccessTokenFromCookie();
  if (!token) return null;
  return verifyAccessToken(token);
}

/**
 * 透明刷新：用 refresh token cookie 验证并签发新 access token
 * - 验证 refresh token（查库）
 * - 签发新 access token 并设置 cookie
 * - 如果 refresh token 过了一半寿命，自动滚动续期
 */
async function tryTransparentRefresh(): Promise<JWTPayload | null> {
  const plainToken = await getRefreshTokenFromCookie();
  if (!plainToken) return null;

  const tokenLookup = crypto.createHash("sha256").update(plainToken).digest("hex");

  const matchedToken = await db.query.refreshTokens.findFirst({
    where: and(
      eq(refreshTokens.tokenLookup, tokenLookup),
      gt(refreshTokens.expiresAt, new Date())
    ),
    with: { user: true },
  });

  if (!matchedToken || !(await bcrypt.compare(plainToken, matchedToken.tokenHash))) {
    return null;
  }

  const user = matchedToken.user;
  const payload: JWTPayload = {
    userId: user.id,
    username: user.username,
    role: user.role,
  };

  // 签发新 access token 并设置 cookie
  const newAccessToken = await signAccessToken(payload);
  await setAuthCookies(newAccessToken);

  // 检查 refresh token 是否需要滚动续期（剩余时间不足一半）
  const remainingMs = matchedToken.expiresAt.getTime() - Date.now();
  if (remainingMs < REFRESH_TOKEN_RENEW_THRESHOLD_MS) {
    const newPlainToken = crypto.randomBytes(32).toString("hex");
    const newTokenHash = await bcrypt.hash(newPlainToken, 10);
    const newTokenLookup = crypto.createHash("sha256").update(newPlainToken).digest("hex");
    const newExpiresAt = new Date(Date.now() + REFRESH_TOKEN_TOTAL_MS);

    await db.transaction(async (tx) => {
      await tx.insert(refreshTokens).values({
        userId: user.id,
        tokenHash: newTokenHash,
        tokenLookup: newTokenLookup,
        deviceName: matchedToken.deviceName,
        expiresAt: newExpiresAt,
      });
      await tx.delete(refreshTokens).where(eq(refreshTokens.id, matchedToken.id));
    });

    // 设置新的 refresh token cookie
    await setAuthCookies(newAccessToken, newPlainToken);
  }

  return payload;
}

/**
 * 从请求中获取认证信息（服务端透明处理双 token）
 *
 * 流程：
 * 1. Bearer token（App 客户端）→ 验证 JWT，不查库
 * 2. Access token cookie（Web）→ 验证 JWT，不查库
 * 3. 以上都失败 → 用 refresh token cookie 透明刷新（查库，签发新 JWT，设 cookie）
 *
 * 对调用方完全透明：返回 JWTPayload 就是已认证，null 就是未认证
 */
export async function getAuthFromRequest(
  req: Request
): Promise<JWTPayload | null> {
  // 1. 检查 Authorization: Bearer <token> header（App 客户端）
  const authHeader = req.headers.get("authorization");
  if (authHeader?.startsWith("Bearer ")) {
    const token = authHeader.slice(7);
    const payload = await verifyAccessToken(token);
    if (payload) return payload;
    // Bearer token 过期，App 客户端需要走 /api/auth/refresh 端点
    // 服务端无法透明刷新（refresh token 不在请求中）
    return null;
  }

  // 2. 检查 access token cookie
  const cookieToken = await getAccessTokenFromCookie();
  if (cookieToken) {
    const payload = await verifyAccessToken(cookieToken);
    if (payload) return payload;
  }

  // 3. JWT 过期或不存在 → 用 refresh token cookie 透明刷新
  return tryTransparentRefresh();
}

/** 检查请求是否来自 App 客户端 */
export function isAppClient(req: Request): boolean {
  return req.headers.get("x-client-type") === "app";
}

/** 从请求验证管理员身份，返回 JWTPayload 或 null */
export async function requireAdmin(req: Request): Promise<JWTPayload | null> {
  const payload = await getAuthFromRequest(req);
  if (!payload || payload.role !== "admin") return null;
  return payload;
}

// ========== Refresh token 滚动续期（供 /api/auth/refresh 端点使用） ==========

/**
 * 检查 refresh token 是否需要续期，如需要则滚动续期
 * 返回新的 refresh token 明文（如果续期了），否则返回 null
 */
export async function maybeRollRefreshToken(
  tokenId: string,
  userId: string,
  deviceName: string,
  expiresAt: Date
): Promise<string | null> {
  const remainingMs = expiresAt.getTime() - Date.now();
  if (remainingMs >= REFRESH_TOKEN_RENEW_THRESHOLD_MS) {
    return null; // 还不需要续期
  }

  const newPlainToken = crypto.randomBytes(32).toString("hex");
  const newTokenHash = await bcrypt.hash(newPlainToken, 10);
  const newTokenLookup = crypto.createHash("sha256").update(newPlainToken).digest("hex");
  const newExpiresAt = new Date(Date.now() + REFRESH_TOKEN_TOTAL_MS);

  await db.transaction(async (tx) => {
    await tx.insert(refreshTokens).values({
      userId,
      tokenHash: newTokenHash,
      tokenLookup: newTokenLookup,
      deviceName,
      expiresAt: newExpiresAt,
    });
    await tx.delete(refreshTokens).where(eq(refreshTokens.id, tokenId));
  });

  return newPlainToken;
}

/**
 * 服务端组件专用：获取当前用户完整信息（含 email）
 * 先通过 JWT 获取 userId，再查库获取最新用户信息
 */
export async function getServerUser(): Promise<{
  id: string;
  username: string;
  email: string;
  role: string;
  githubId: string | null;
  hasLocalPassword: boolean;
} | null> {
  const payload = await getCurrentUser();
  if (!payload) return null;

  const [user] = await db
    .select({
      id: users.id,
      username: users.username,
      email: users.email,
      role: users.role,
      githubId: users.githubId,
      passwordHash: users.passwordHash,
    })
    .from(users)
    .where(eq(users.id, payload.userId))
    .limit(1);

  if (!user) return null;

  return {
    id: user.id,
    username: user.username,
    email: user.email,
    role: user.role,
    githubId: user.githubId,
    hasLocalPassword: Boolean(user.passwordHash),
  };
}
