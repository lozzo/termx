import { db } from "./db";
import { users, referrals, subscriptions } from "./schema";
import { eq, and, sql } from "drizzle-orm";
import crypto from "crypto";

/**
 * 生成 8 位唯一邀请码
 */
export function generateReferralCode(): string {
  return crypto.randomBytes(4).toString("hex"); // 8 hex chars
}

/**
 * 获取或生成用户的邀请码
 */
export async function getOrCreateReferralCode(userId: string): Promise<string> {
  const [user] = await db
    .select({ referralCode: users.referralCode })
    .from(users)
    .where(eq(users.id, userId))
    .limit(1);

  if (user?.referralCode) return user.referralCode;

  // 生成唯一邀请码，重试最多 5 次
  for (let i = 0; i < 5; i++) {
    const code = generateReferralCode();
    try {
      await db
        .update(users)
        .set({ referralCode: code })
        .where(and(eq(users.id, userId), sql`${users.referralCode} IS NULL`));

      const [updated] = await db
        .select({ referralCode: users.referralCode })
        .from(users)
        .where(eq(users.id, userId))
        .limit(1);

      if (updated?.referralCode) return updated.referralCode;
    } catch {
      // unique constraint violation, retry
      continue;
    }
  }

  throw new Error("Failed to generate unique referral code");
}

/**
 * 被邀请人购买订阅时调用：
 * 更新 referral status=completed，给邀请人发放奖励
 */
export async function processReferralReward(inviteeUserId: string): Promise<void> {
  // 查找该用户作为被邀请人的 pending 记录
  const pendingReferrals = await db
    .select()
    .from(referrals)
    .where(
      and(
        eq(referrals.inviteeId, inviteeUserId),
        eq(referrals.status, "pending")
      )
    );

  if (pendingReferrals.length === 0) return;

  for (const referral of pendingReferrals) {
    // 检查邀请人是否有活跃订阅
    const [sub] = await db
      .select()
      .from(subscriptions)
      .where(
        and(
          eq(subscriptions.userId, referral.referrerId),
          eq(subscriptions.status, "active")
        )
      )
      .limit(1);

    const hasActiveSub = sub && sub.currentPeriodEnd > new Date();

    if (hasActiveSub) {
      // 邀请人有活跃订阅，直接延长
      const newEnd = new Date(sub.currentPeriodEnd);
      newEnd.setDate(newEnd.getDate() + referral.rewardDays);

      await db
        .update(subscriptions)
        .set({ currentPeriodEnd: newEnd })
        .where(eq(subscriptions.id, sub.id));

      await db
        .update(referrals)
        .set({
          status: "completed",
          rewarded: true,
          rewardedAt: new Date(),
        })
        .where(eq(referrals.id, referral.id));

      console.log(
        `[Referral] Rewarded referrer ${referral.referrerId}: +${referral.rewardDays} days (extended to ${newEnd.toISOString()})`
      );
    } else {
      // 邀请人无活跃订阅，标记 completed 但待发放
      await db
        .update(referrals)
        .set({ status: "completed" })
        .where(eq(referrals.id, referral.id));

      console.log(
        `[Referral] Referral completed for referrer ${referral.referrerId}, reward pending (no active subscription)`
      );
    }
  }
}

/**
 * 邀请人购买订阅时调用：
 * 查找所有 completed 且 rewarded=false 的记录，累加天数到订阅结束时间
 */
export async function applyPendingReferralRewards(referrerUserId: string): Promise<void> {
  const pendingRewards = await db
    .select()
    .from(referrals)
    .where(
      and(
        eq(referrals.referrerId, referrerUserId),
        eq(referrals.status, "completed"),
        eq(referrals.rewarded, false)
      )
    );

  if (pendingRewards.length === 0) return;

  const totalDays = pendingRewards.reduce((sum, r) => sum + r.rewardDays, 0);

  // 获取当前订阅
  const [sub] = await db
    .select()
    .from(subscriptions)
    .where(
      and(
        eq(subscriptions.userId, referrerUserId),
        eq(subscriptions.status, "active")
      )
    )
    .limit(1);

  if (!sub) return;

  const newEnd = new Date(sub.currentPeriodEnd);
  newEnd.setDate(newEnd.getDate() + totalDays);

  await db
    .update(subscriptions)
    .set({ currentPeriodEnd: newEnd })
    .where(eq(subscriptions.id, sub.id));

  // 批量标记为已发放
  const ids = pendingRewards.map((r) => r.id);
  for (const id of ids) {
    await db
      .update(referrals)
      .set({ rewarded: true, rewardedAt: new Date() })
      .where(eq(referrals.id, id));
  }

  console.log(
    `[Referral] Applied ${totalDays} pending reward days to user ${referrerUserId} (${pendingRewards.length} referrals)`
  );
}

/**
 * 查询邀请统计
 */
export async function getReferralStats(userId: string) {
  const allReferrals = await db
    .select({
      id: referrals.id,
      status: referrals.status,
      rewarded: referrals.rewarded,
      rewardDays: referrals.rewardDays,
      createdAt: referrals.createdAt,
    })
    .from(referrals)
    .where(eq(referrals.referrerId, userId));

  const totalInvited = allReferrals.length;
  const completedCount = allReferrals.filter((r) => r.status === "completed").length;
  const rewardedDays = allReferrals
    .filter((r) => r.rewarded)
    .reduce((sum, r) => sum + r.rewardDays, 0);
  const pendingDays = allReferrals
    .filter((r) => r.status === "completed" && !r.rewarded)
    .reduce((sum, r) => sum + r.rewardDays, 0);

  return {
    totalInvited,
    completedCount,
    rewardedDays,
    pendingDays,
  };
}
