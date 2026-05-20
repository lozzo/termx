import { cacheManager } from "./index";
import { cacheKeys } from "./keys";

async function bumpBatchGeneration() {
  const key = cacheKeys.batchGeneration();
  const current = (await cacheManager.get<number>(key)) ?? 0;
  await cacheManager.set(key, current + 1, 365 * 24 * 60 * 60 * 1000);
}

export async function getSubscriptionBatchGeneration() {
  const key = cacheKeys.batchGeneration();
  const current = (await cacheManager.get<number>(key)) ?? 0;
  if (current === 0) {
    await cacheManager.set(key, 1, 365 * 24 * 60 * 60 * 1000);
    return 1;
  }
  return current;
}

export async function invalidatePlans() {
  await Promise.all([
    cacheManager.delete(cacheKeys.billingPlans()),
    cacheManager.delete(cacheKeys.adminPlans()),
  ]);
}

export async function invalidateSubscription(userId: string) {
  await Promise.all([
    cacheManager.delete(cacheKeys.subscription(userId)),
    cacheManager.delete(cacheKeys.userSubscriptionSummary(userId)),
  ]);
  await bumpBatchGeneration();
}

export async function invalidateDashboardStats(userId: string) {
  await cacheManager.delete(cacheKeys.dashboardStats(userId));
}

export async function invalidateReferralStats(userId: string) {
  await cacheManager.delete(cacheKeys.referralStats(userId));
}

export async function invalidateUserAgents(userId: string) {
  await Promise.all([
    cacheManager.delete(cacheKeys.userAgents(userId)),
    cacheManager.delete(cacheKeys.dashboardStats(userId)),
    cacheManager.delete(cacheKeys.adminAgents()),
  ]);
}

export async function invalidateHubsDiscover() {
  await cacheManager.delete(cacheKeys.hubsDiscover());
}

export async function invalidateAdminUsers() {
  await cacheManager.delete(cacheKeys.adminUsers());
}

export async function invalidateAdminOrders() {
  await cacheManager.delete(cacheKeys.adminOrders());
}

export async function invalidateAdminAgents() {
  await cacheManager.delete(cacheKeys.adminAgents());
}

export async function invalidateAdminHubs() {
  await Promise.all([
    cacheManager.delete(cacheKeys.adminHubs()),
    cacheManager.delete(cacheKeys.hubsDiscover()),
  ]);
}

export async function invalidateAdminSubscriptions() {
  await cacheManager.delete(cacheKeys.adminSubscriptions());
  await bumpBatchGeneration();
}

export async function invalidateAdminAgentReleases() {
  await cacheManager.delete(cacheKeys.adminAgentReleases());
}

export async function invalidateAdminAppReleases() {
  await cacheManager.delete(cacheKeys.adminAppReleases());
}

export async function invalidateAdminPromoCodes() {
  await cacheManager.delete(cacheKeys.adminPromoCodes());
}

export async function invalidateAdminOverrides() {
  await cacheManager.delete(cacheKeys.adminOverrides());
  await bumpBatchGeneration();
}

export async function invalidateAgentLatestRelease(os: string, arch: string) {
  await Promise.all([
    cacheManager.delete(cacheKeys.agentLatestRelease(os, arch)),
    cacheManager.delete(cacheKeys.adminAgentReleases()),
  ]);
}

export async function invalidateAppLatestRelease(platform: string, type: string) {
  await Promise.all([
    cacheManager.delete(cacheKeys.appLatestRelease(platform, type)),
    cacheManager.delete(cacheKeys.adminAppReleases()),
  ]);
}
