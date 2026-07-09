import crypto from "crypto";

const CACHE_PREFIX = process.env.CACHE_PREFIX || "termx:web";
const LOCK_PREFIX = process.env.CACHE_LOCK_PREFIX || "termx:lock";

function buildKey(prefix: string, ...parts: Array<string | number | boolean>) {
  return [prefix, ...parts].join(":");
}

function normalizedListHash(values: string[]) {
  const normalized = [...new Set(values)].sort().join(",");
  return crypto.createHash("sha1").update(normalized).digest("hex");
}

export const cacheKeys = {
  billingPlans: () => buildKey(CACHE_PREFIX, "billing", "plans", "active", "v1"),
  subscription: (userId: string) => buildKey(CACHE_PREFIX, "subscription", "user", userId, "active", "v1"),
  dashboardStats: (userId: string) => buildKey(CACHE_PREFIX, "dashboard", "user", userId, "stats", "v1"),
  referralStats: (userId: string) => buildKey(CACHE_PREFIX, "referral", "user", userId, "stats", "v1"),
  userAgents: (userId: string) => buildKey(CACHE_PREFIX, "agents", "user", userId, "list", "v1"),
  userSubscriptionSummary: (userId: string) => buildKey(CACHE_PREFIX, "billing", "subscription", "user", userId, "v1"),
  unsubscribedUsers: (userIds: string[], generation: number) => buildKey(CACHE_PREFIX, "subscription", "batch", "unsubscribed", generation, normalizedListHash(userIds), "v1"),
  bandwidthLimits: (userIds: string[], generation: number) => buildKey(CACHE_PREFIX, "subscription", "batch", "bandwidth", generation, normalizedListHash(userIds), "v1"),
  hubsDiscover: () => buildKey(CACHE_PREFIX, "hubs", "discover", "online", "v1"),
  agentLatestRelease: (os: string, arch: string) => buildKey(CACHE_PREFIX, "release", "agent", "latest", "os", os, "arch", arch, "v1"),
  appLatestRelease: (platform: string, type: string) => buildKey(CACHE_PREFIX, "release", "app", "latest", "platform", platform, "type", type, "v1"),
  adminUsers: () => buildKey(CACHE_PREFIX, "admin", "users", "list", "v1"),
  adminOrders: () => buildKey(CACHE_PREFIX, "admin", "orders", "list", "v1"),
  adminAgents: () => buildKey(CACHE_PREFIX, "admin", "agents", "list", "v1"),
  adminHubs: () => buildKey(CACHE_PREFIX, "admin", "hubs", "list", "v1"),
  adminSubscriptions: () => buildKey(CACHE_PREFIX, "admin", "subscriptions", "list", "v1"),
  adminPlans: () => buildKey(CACHE_PREFIX, "admin", "plans", "list", "v1"),
  adminAgentReleases: () => buildKey(CACHE_PREFIX, "admin", "releases", "agent", "list", "v1"),
  adminAppReleases: () => buildKey(CACHE_PREFIX, "admin", "releases", "app", "list", "v1"),
  adminPromoCodes: () => buildKey(CACHE_PREFIX, "admin", "promo-codes", "list", "v1"),
  adminOverrides: () => buildKey(CACHE_PREFIX, "admin", "overrides", "list", "v1"),
  batchGeneration: () => buildKey(CACHE_PREFIX, "subscription", "batch", "generation", "v1"),
};

export const lockKeys = {
  forCacheKey: (cacheKey: string) => cacheKey.replace(CACHE_PREFIX, LOCK_PREFIX),
};
