const devDefaults: Record<string, string> = {
  JWT_SECRET: "termx-development-jwt-secret-change-me",
  HUB_SECRET: "termx-development-hub-secret-change-me",
};

function requireEnv(name: string): string {
  const val = process.env[name];
  if (val) return val;
  if (process.env.NODE_ENV !== "production" && devDefaults[name]) {
    return devDefaults[name];
  }
  throw new Error(`Missing required env: ${name}`);
}

export function getJwtSecret(): Uint8Array {
  return new TextEncoder().encode(requireEnv("JWT_SECRET"));
}

export function getAppUrl(): string | undefined {
  const val = process.env.APP_URL?.trim();
  return val ? val.replace(/\/$/, "") : undefined;
}

export function getHubSecret(): string {
  return requireEnv("HUB_SECRET");
}

export function getGithubClientId(): string {
  return requireEnv("GITHUB_CLIENT_ID");
}

export function getGithubClientSecret(): string {
  return requireEnv("GITHUB_CLIENT_SECRET");
}

export function isGithubOAuthConfigured(): boolean {
  return Boolean(process.env.GITHUB_CLIENT_ID && process.env.GITHUB_CLIENT_SECRET);
}
