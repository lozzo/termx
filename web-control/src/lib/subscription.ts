import { db } from "./db";
import { subscriptions, plans, userOverrides, type UserOverridesData } from "./schema";
import { eq, and, gt, or, isNull } from "drizzle-orm";
import { cacheKeys, cacheManager, cacheTtl } from "./cache-system";
import { getSubscriptionBatchGeneration } from "./cache-system/invalidation";

export interface ActiveSubscription {
  planId: string;
  planName: string;
  maxAgents: number;
  maxServers: number;
  relayBandwidthKbps: number;
  allowRelayTransfer: boolean;
  currentPeriodEnd: Date;
}

const noPaySubscription: ActiveSubscription = {
  planId: "dev",
  planName: "开发版",
  maxAgents: 1000,
  maxServers: 1000,
  relayBandwidthKbps: 0,
  allowRelayTransfer: true,
  currentPeriodEnd: new Date("2999-12-31T23:59:59Z"),
};

export function isBillingDisabled(): boolean {
  return process.env.DISABLE_BILLING !== "false";
}

export function getNoPaySubscription(): ActiveSubscription {
  return noPaySubscription;
}

/**
 * 查询用户未过期的 override，返回解析后的对象或 null
 */
export async function getUserOverrides(
  userId: string
): Promise<UserOverridesData | null> {
  const row = await db
    .select({ overrides: userOverrides.overrides, expiresAt: userOverrides.expiresAt })
    .from(userOverrides)
    .where(
      and(
        eq(userOverrides.userId, userId),
        or(isNull(userOverrides.expiresAt), gt(userOverrides.expiresAt, new Date()))
      )
    )
    .limit(1);

  if (row.length === 0) return null;
  return row[0].overrides;
}

/**
 * 获取用户的有效订阅信息
 * 检查 status === "active" 且 currentPeriodEnd > now
 * 合并 userOverrides 中的特权值
 * @returns 有效订阅信息，或 null
 */
export async function getActiveSubscription(
  userId: string
): Promise<ActiveSubscription | null> {
  if (isBillingDisabled()) {
    const overrides = await getUserOverrides(userId);
    return {
      ...noPaySubscription,
      maxAgents: overrides?.maxAgents ?? noPaySubscription.maxAgents,
      maxServers: overrides?.maxServers ?? noPaySubscription.maxServers,
      relayBandwidthKbps: overrides?.relayBandwidthKbps ?? noPaySubscription.relayBandwidthKbps,
      allowRelayTransfer: overrides?.allowRelayTransfer ?? noPaySubscription.allowRelayTransfer,
    };
  }

  return cacheManager.getOrLoad({
    key: cacheKeys.subscription(userId),
    ttlMs: cacheTtl.userMedium,
    negativeTtlMs: cacheTtl.negativeShort,
    loader: async () => {
      const result = await db
        .select({
          planId: subscriptions.planId,
          planName: plans.name,
          maxAgents: plans.maxAgents,
          maxServers: plans.maxServers,
          relayBandwidthKbps: plans.relayBandwidthKbps,
          currentPeriodEnd: subscriptions.currentPeriodEnd,
        })
        .from(subscriptions)
        .innerJoin(plans, eq(subscriptions.planId, plans.id))
        .where(
          and(
            eq(subscriptions.userId, userId),
            eq(subscriptions.status, "active"),
            gt(subscriptions.currentPeriodEnd, new Date())
          )
        )
        .limit(1);

      if (result.length === 0) {
        return null;
      }

      const sub = result[0];
      const overrides = await getUserOverrides(userId);

      return {
        planId: sub.planId,
        planName: sub.planName,
        maxAgents: overrides?.maxAgents ?? sub.maxAgents,
        maxServers: overrides?.maxServers ?? sub.maxServers,
        relayBandwidthKbps: overrides?.relayBandwidthKbps ?? sub.relayBandwidthKbps,
        allowRelayTransfer: overrides?.allowRelayTransfer ?? false,
        currentPeriodEnd: sub.currentPeriodEnd,
      };
    },
  });
}

/**
 * 批量检查多个用户的订阅状态
 * 有未过期 override 的用户也视为有效
 * @returns 无有效订阅的用户 ID 集合
 */
export async function getUnsubscribedUserIds(
  userIds: string[]
): Promise<Set<string>> {
  if (userIds.length === 0) return new Set();
  if (isBillingDisabled()) return new Set();

  const uniqueIds = [...new Set(userIds)];
  const generation = await getSubscriptionBatchGeneration();

  return cacheManager.getOrLoad({
    key: cacheKeys.unsubscribedUsers(uniqueIds, generation),
    ttlMs: cacheTtl.heartbeat,
    loader: async () => {
      const activeSubscriptions = await db
        .select({ userId: subscriptions.userId })
        .from(subscriptions)
        .where(
          and(
            eq(subscriptions.status, "active"),
            gt(subscriptions.currentPeriodEnd, new Date())
          )
        );

      const subscribedSet = new Set(activeSubscriptions.map((s: { userId: string }) => s.userId));

      const activeOverrides = await db
        .select({ userId: userOverrides.userId })
        .from(userOverrides)
        .where(
          or(isNull(userOverrides.expiresAt), gt(userOverrides.expiresAt, new Date()))
        );

      for (const o of activeOverrides) {
        subscribedSet.add(o.userId);
      }

      const unsubscribed = new Set<string>();
      for (const id of uniqueIds) {
        if (!subscribedSet.has(id)) {
          unsubscribed.add(id);
        }
      }
      return Array.from(unsubscribed);
    },
  }).then((ids) => new Set(ids));
}

/**
 * 批量查询用户的带宽限速值（kbps）
 * 合并 plans + userOverrides，override 优先
 * @returns Map<userId, kbps>，只包含 kbps > 0 的用户
 */
export async function getUserBandwidthLimits(
  userIds: string[]
): Promise<Map<string, number>> {
  if (userIds.length === 0) return new Map();
  if (isBillingDisabled()) return new Map();

  const uniqueIds = [...new Set(userIds)];
  const generation = await getSubscriptionBatchGeneration();

  return cacheManager.getOrLoad({
    key: cacheKeys.bandwidthLimits(uniqueIds, generation),
    ttlMs: cacheTtl.heartbeat,
    loader: async () => {
      const result = new Map<string, number>();

      const activeSubs = await db
        .select({
          userId: subscriptions.userId,
          relayBandwidthKbps: plans.relayBandwidthKbps,
        })
        .from(subscriptions)
        .innerJoin(plans, eq(subscriptions.planId, plans.id))
        .where(
          and(
            eq(subscriptions.status, "active"),
            gt(subscriptions.currentPeriodEnd, new Date())
          )
        );

      for (const sub of activeSubs) {
        if (uniqueIds.includes(sub.userId) && sub.relayBandwidthKbps > 0) {
          result.set(sub.userId, sub.relayBandwidthKbps);
        }
      }

      const activeOvr = await db
        .select({
          userId: userOverrides.userId,
          overrides: userOverrides.overrides,
        })
        .from(userOverrides)
        .where(
          or(isNull(userOverrides.expiresAt), gt(userOverrides.expiresAt, new Date()))
        );

      for (const o of activeOvr) {
        if (!uniqueIds.includes(o.userId)) continue;
        const kbps = o.overrides?.relayBandwidthKbps;
        if (kbps !== undefined && kbps !== null) {
          if (kbps > 0) {
            result.set(o.userId, kbps);
          } else {
            result.delete(o.userId);
          }
        }
      }

      return Array.from(result.entries());
    },
  }).then((entries) => new Map(entries));
}
