import { getAppUrl } from "./config";

const INVALID_REDIRECT_HOSTS = new Set(["0.0.0.0", "::", "[::]"]);

function getFirstHeaderValue(headers: Headers, name: string): string | null {
  const raw = headers.get(name);
  if (!raw) return null;

  const value = raw.split(",")[0]?.trim();
  return value || null;
}

function normalizeProtocol(protocol?: string | null): string {
  if (!protocol) return "https";
  return protocol.replace(/:$/, "").trim() || "https";
}

function toSafeOrigin(value?: string | null): string | undefined {
  if (!value) return undefined;

  try {
    const url = new URL(value);
    if (INVALID_REDIRECT_HOSTS.has(url.hostname)) {
      return undefined;
    }
    return url.origin;
  } catch {
    return undefined;
  }
}

function buildOrigin(protocol: string, host?: string | null): string | undefined {
  if (!host) return undefined;
  return toSafeOrigin(`${protocol}://${host}`);
}

export function sanitizeRedirectPath(value?: string | null): string | undefined {
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return undefined;
  }

  return value;
}

export function getSafeClientRedirectPath(
  value?: string | null,
  fallback = "/dashboard"
): string {
  return sanitizeRedirectPath(value) || fallback;
}

export function getRequestOrigin(request: Pick<Request, "url" | "headers">): string {
  const configuredOrigin = toSafeOrigin(getAppUrl());
  if (configuredOrigin) {
    return configuredOrigin;
  }

  const requestUrl = new URL(request.url);
  const requestProtocol = normalizeProtocol(requestUrl.protocol);
  const forwardedProtocol = normalizeProtocol(
    getFirstHeaderValue(request.headers, "x-forwarded-proto") || requestProtocol
  );

  const forwardedOrigin = buildOrigin(
    forwardedProtocol,
    getFirstHeaderValue(request.headers, "x-forwarded-host")
  );
  if (forwardedOrigin) {
    return forwardedOrigin;
  }

  const hostOrigin = buildOrigin(
    forwardedProtocol,
    getFirstHeaderValue(request.headers, "host")
  );
  if (hostOrigin) {
    return hostOrigin;
  }

  return toSafeOrigin(requestUrl.origin) || requestUrl.origin;
}

export function buildRequestUrl(
  request: Pick<Request, "url" | "headers">,
  path: string
): URL {
  return new URL(path, `${getRequestOrigin(request)}/`);
}
