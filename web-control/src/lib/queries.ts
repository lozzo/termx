import { db } from "./db";
import {
  agents,
  users,
  hubs,
  plans,
  subscriptions,
  orders,
  accessTokens,
  promoCodes,
  agentReleases,
  appReleases,
  userOverrides,
} from "./schema";
import { eq, and, count, desc, asc, sql, gt } from "drizzle-orm";
import { getOrCreateReferralCode, getReferralStats } from "./referral.service";
import { cacheKeys, cacheManager, cacheTtl } from "./cache-system";
import { getNoPaySubscription, isBillingDisabled } from "./subscription";

// ==================== Dashboard ====================

export async function getDashboardStats(userId: string) {
  return cacheManager.getOrLoad({
    key: cacheKeys.dashboardStats(userId),
    ttlMs: cacheTtl.userShort,
    loader: async () => {
      const [[totalResult], [onlineResult]] = await Promise.all([
        db
          .select({ value: count() })
          .from(agents)
          .where(eq(agents.userId, userId)),
        db
          .select({ value: count() })
          .from(agents)
          .where(and(eq(agents.userId, userId), eq(agents.online, true))),
      ]);

      return {
        totalAgents: totalResult.value,
        onlineAgents: onlineResult.value,
      };
    },
  });
}

// ==================== Billing ====================

export async function getUserSubscription(userId: string) {
  if (isBillingDisabled()) {
    const sub = getNoPaySubscription();
    return {
      id: "dev-no-pay",
      planId: sub.planId,
      planName: sub.planName,
      status: "active",
      currentPeriodEnd: sub.currentPeriodEnd,
      billingCycle: "none",
    };
  }

  return cacheManager.getOrLoad({
    key: cacheKeys.userSubscriptionSummary(userId),
    ttlMs: cacheTtl.userMedium,
    negativeTtlMs: cacheTtl.negativeShort,
    loader: async () => {
      const sub = await db.query.subscriptions.findFirst({
        where: eq(subscriptions.userId, userId),
        with: { plan: true, order: { columns: { billingCycle: true } } },
      });

      if (!sub) return null;

      return {
        id: sub.id,
        planId: sub.planId,
        planName: sub.plan.name,
        status: sub.status,
        currentPeriodEnd: sub.currentPeriodEnd,
        billingCycle: sub.order.billingCycle,
      };
    },
  });
}

export async function getActivePlans() {
  return cacheManager.getOrLoad({
    key: cacheKeys.billingPlans(),
    ttlMs: cacheTtl.publicHot,
    loader: async () => {
      const result = await db
        .select()
        .from(plans)
        .where(eq(plans.active, true))
        .orderBy(asc(plans.price));

      return result.map((p) => ({
        id: p.id,
        name: p.name,
        price: p.price,
        priceYearly: p.priceYearly,
        features: JSON.parse(p.features),
        maxServers: p.maxServers,
        maxAgents: p.maxAgents,
      }));
    },
  });
}

// ==================== Agents ====================

export async function getUserAgents(userId: string) {
  return cacheManager.getOrLoad({
    key: cacheKeys.userAgents(userId),
    ttlMs: cacheTtl.userShort,
    loader: async () => {
      const result = await db.query.agents.findMany({
        where: eq(agents.userId, userId),
        with: {
          hub: {
            columns: { id: true, name: true, region: true, httpUrl: true },
          },
          token: {
            columns: { id: true, name: true },
          },
        },
        orderBy: desc(agents.createdAt),
      });

      return result.map((a) => ({
        id: a.id,
        name: a.name,
        hostname: a.hostname,
        osInfo: a.osInfo,
        labels: a.labels,
        online: a.online,
        hubId: a.hubId,
        hubHttpUrl: a.hub?.httpUrl || null,
        tokenId: a.tokenId,
        tokenName: a.token?.name || null,
        lastSeen: a.lastSeen,
        createdAt: a.createdAt,
      }));
    },
  });
}

// ==================== Tokens ====================

export async function getUserTokens(userId: string) {
  const tokens = await db.query.accessTokens.findMany({
    where: eq(accessTokens.userId, userId),
    with: {
      agents: {
        columns: {
          id: true,
          name: true,
          hostname: true,
          osInfo: true,
          online: true,
          lastSeen: true,
        },
      },
    },
    orderBy: desc(accessTokens.createdAt),
  });

  return tokens.map((t) => ({
    id: t.id,
    name: t.name,
    token: t.token || "",
    expiresAt: t.expiresAt,
    createdAt: t.createdAt,
    agentCount: t.agents.length,
    agents: t.agents,
  }));
}

// ==================== Referral ====================

export async function getUserReferralStats(userId: string) {
  return cacheManager.getOrLoad({
    key: cacheKeys.referralStats(userId),
    ttlMs: cacheTtl.userMedium,
    loader: async () => {
      const [referralCode, stats] = await Promise.all([
        getOrCreateReferralCode(userId),
        getReferralStats(userId),
      ]);

      return {
        referralCode,
        ...stats,
      };
    },
  });
}

// ==================== Admin ====================

export async function getAdminUsers() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminUsers(),
    ttlMs: cacheTtl.adminShort,
    loader: async () =>
      db
        .select({
          id: users.id,
          username: users.username,
          email: users.email,
          role: users.role,
          createdAt: users.createdAt,
          subscriptionStatus: subscriptions.status,
          subscriptionEnd: subscriptions.currentPeriodEnd,
          agentCount:
            sql<number>`(SELECT COUNT(*) FROM agents WHERE agents.user_id = ${users.id})`.as(
              "agent_count"
            ),
        })
        .from(users)
        .leftJoin(subscriptions, eq(users.id, subscriptions.userId))
        .orderBy(desc(users.createdAt)),
  });
}

export async function getAdminOrders() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminOrders(),
    ttlMs: cacheTtl.adminShort,
    loader: async () => {
      const [stats] = await db
        .select({
          totalRevenue:
            sql<number>`COALESCE(SUM(CASE WHEN ${orders.status} = 'paid' THEN ${orders.amount} - ${orders.discountAmount} ELSE 0 END), 0)`,
          monthlyRevenue:
            sql<number>`COALESCE(SUM(CASE WHEN ${orders.status} = 'paid' AND ${orders.paidAt} >= unixepoch('subsec', 'start of month') * 1000 THEN ${orders.amount} - ${orders.discountAmount} ELSE 0 END), 0)`,
          totalOrders: sql<number>`COUNT(*)`,
          paidOrders:
            sql<number>`SUM(CASE WHEN ${orders.status} = 'paid' THEN 1 ELSE 0 END)`,
        })
        .from(orders);

      const list = await db
        .select({
          id: orders.id,
          userId: orders.userId,
          username: users.username,
          planName: plans.name,
          amount: orders.amount,
          discountAmount: orders.discountAmount,
          currency: orders.currency,
          billingCycle: orders.billingCycle,
          status: orders.status,
          paymentMethod: orders.paymentMethod,
          promoCode: promoCodes.code,
          createdAt: orders.createdAt,
          paidAt: orders.paidAt,
        })
        .from(orders)
        .leftJoin(users, eq(orders.userId, users.id))
        .leftJoin(plans, eq(orders.planId, plans.id))
        .leftJoin(promoCodes, eq(orders.promoCodeId, promoCodes.id))
        .orderBy(desc(orders.createdAt));

      return { stats, orders: list };
    },
  });
}

export async function getAdminAgents() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminAgents(),
    ttlMs: cacheTtl.adminShort,
    loader: async () =>
      db
        .select({
          id: agents.id,
          name: agents.name,
          userId: agents.userId,
          username: users.username,
          hostname: agents.hostname,
          osInfo: agents.osInfo,
          online: agents.online,
          hubId: agents.hubId,
          hubName: hubs.name,
          pendingKick: agents.pendingKick,
          sessionBytesIn: agents.sessionBytesIn,
          sessionBytesOut: agents.sessionBytesOut,
          sessionStartedAt: agents.sessionStartedAt,
          lastSeen: agents.lastSeen,
          createdAt: agents.createdAt,
        })
        .from(agents)
        .leftJoin(users, eq(agents.userId, users.id))
        .leftJoin(hubs, eq(agents.hubId, hubs.id))
        .orderBy(desc(agents.createdAt)),
  });
}

export async function getAdminHubs() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminHubs(),
    ttlMs: cacheTtl.adminShort,
    loader: async () => db.select().from(hubs).orderBy(desc(hubs.createdAt)),
  });
}

export async function getAdminSubscriptions() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminSubscriptions(),
    ttlMs: cacheTtl.adminShort,
    loader: async () =>
      db
        .select({
          id: subscriptions.id,
          userId: subscriptions.userId,
          username: users.username,
          email: users.email,
          planId: subscriptions.planId,
          planName: plans.name,
          status: subscriptions.status,
          currentPeriodEnd: subscriptions.currentPeriodEnd,
          createdAt: subscriptions.createdAt,
        })
        .from(subscriptions)
        .leftJoin(users, eq(subscriptions.userId, users.id))
        .leftJoin(plans, eq(subscriptions.planId, plans.id))
        .orderBy(desc(subscriptions.createdAt)),
  });
}

export async function getAdminPlans() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminPlans(),
    ttlMs: cacheTtl.adminShort,
    loader: async () => db.select().from(plans).orderBy(desc(plans.createdAt)),
  });
}

export async function getAdminAgentReleases() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminAgentReleases(),
    ttlMs: cacheTtl.adminShort,
    loader: async () => db.select().from(agentReleases).orderBy(desc(agentReleases.createdAt)),
  });
}

export async function getAdminAppReleases() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminAppReleases(),
    ttlMs: cacheTtl.adminShort,
    loader: async () => db.select().from(appReleases).orderBy(desc(appReleases.createdAt)),
  });
}

export async function getAdminPromoCodes() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminPromoCodes(),
    ttlMs: cacheTtl.adminShort,
    loader: async () => db.select().from(promoCodes).orderBy(desc(promoCodes.createdAt)),
  });
}

export async function getAdminUserOverrides() {
  return cacheManager.getOrLoad({
    key: cacheKeys.adminOverrides(),
    ttlMs: cacheTtl.adminShort,
    loader: async () =>
      db
        .select({
          id: userOverrides.id,
          userId: userOverrides.userId,
          username: users.username,
          email: users.email,
          overrides: userOverrides.overrides,
          note: userOverrides.note,
          expiresAt: userOverrides.expiresAt,
          createdAt: userOverrides.createdAt,
          updatedAt: userOverrides.updatedAt,
        })
        .from(userOverrides)
        .leftJoin(users, eq(userOverrides.userId, users.id))
        .orderBy(desc(userOverrides.createdAt)),
  });
}
