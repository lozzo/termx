import { NextRequest, NextResponse } from "next/server";
import { jwtVerify } from "jose";
import { getJwtSecret } from "./lib/config";
import { buildRequestUrl } from "./lib/url";

const protectedPaths = ["/dashboard", "/device-login"];
const authPaths = ["/login", "/register", "/forgot-password"];

function getAllowedOrigins(): string[] {
  const env = process.env.ALLOWED_ORIGINS;
  if (!env) return [];
  return env.split(",").map((s) => s.trim()).filter(Boolean);
}

function handleCors(request: NextRequest, response: NextResponse): NextResponse {
  const origin = request.headers.get("origin");
  if (!origin) return response;

  const allowed = getAllowedOrigins();
  if (allowed.length === 0 || allowed.includes(origin)) {
    response.headers.set("Access-Control-Allow-Origin", origin);
    response.headers.set("Access-Control-Allow-Credentials", "true");
    response.headers.set(
      "Access-Control-Allow-Headers",
      "Authorization, Content-Type, X-Client-Type, X-Hub-Secret, X-TermX-Hub-Secret"
    );
    response.headers.set(
      "Access-Control-Allow-Methods",
      "GET, POST, PUT, DELETE, OPTIONS"
    );
  }

  return response;
}

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const pathWithSearch = `${pathname}${request.nextUrl.search}`;

  // CORS: 对 /api/ 路由处理
  if (pathname.startsWith("/api/")) {
    // OPTIONS 预检请求
    if (request.method === "OPTIONS") {
      const response = new NextResponse(null, { status: 204 });
      return handleCors(request, response);
    }

    // 普通 API 请求添加 CORS 头
    const response = NextResponse.next();
    return handleCors(request, response);
  }

  // 非 API 路由：认证检查
  const accessToken = request.cookies.get("termx_access_token")?.value;
  const refreshToken = request.cookies.get("termx_refresh_token")?.value;
  let isAuthenticated = false;
  let needsRefresh = false;

  if (accessToken) {
    try {
      await jwtVerify(accessToken, getJwtSecret());
      isAuthenticated = true;
    } catch {
      // access token 过期，但有 refresh token → 需要刷新
      if (refreshToken) {
        needsRefresh = true;
      }
    }
  } else if (refreshToken) {
    // 没有 access token 但有 refresh token → 需要刷新
    needsRefresh = true;
  }

  // 已登录用户访问登录/注册页 -> 直接进入 dashboard
  if (isAuthenticated && authPaths.some((p) => pathname.startsWith(p))) {
    return NextResponse.redirect(buildRequestUrl(request, "/dashboard"));
  }

  // access token 过期但 refresh token 仍在：先走刷新路由，不要直接跳 dashboard
  if (needsRefresh && authPaths.some((p) => pathname.startsWith(p))) {
    const url = buildRequestUrl(request, "/api/auth/refresh-redirect");
    url.searchParams.set("from", "/dashboard");
    return NextResponse.redirect(url);
  }

  // 访问受保护路由时，如果需要刷新 token，重定向到刷新端点
  if (needsRefresh && protectedPaths.some((p) => pathname.startsWith(p))) {
    const url = buildRequestUrl(request, "/api/auth/refresh-redirect");
    url.searchParams.set("from", pathWithSearch);
    return NextResponse.redirect(url);
  }

  // 未登录用户访问受保护页面 -> 跳转 login
  if (!isAuthenticated && !needsRefresh && protectedPaths.some((p) => pathname.startsWith(p))) {
    const url = buildRequestUrl(request, "/login");
    url.searchParams.set("from", pathWithSearch);
    return NextResponse.redirect(url);
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    "/dashboard/:path*",
    "/device-login",
    "/login",
    "/register",
    "/forgot-password",
    "/api/:path*",
  ],
};
