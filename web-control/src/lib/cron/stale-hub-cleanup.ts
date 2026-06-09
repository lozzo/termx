import { db } from "@/lib/db";
import { hubs, agents } from "@/lib/schema";
import { eq, and, inArray, lt } from "drizzle-orm";

const STALE_THRESHOLD_MS = 30 * 1000; // 30 秒未心跳视为离线
const CLEANUP_INTERVAL_MS = 10 * 1000; // 每 10 秒检查一次

async function cleanupStaleHubs() {
  try {
    const cutoff = new Date(Date.now() - STALE_THRESHOLD_MS);

    const staleHubs = await db
      .select({ id: hubs.id })
      .from(hubs)
      .where(and(eq(hubs.status, "online"), lt(hubs.lastHeartbeat, cutoff)));

    if (staleHubs.length === 0) return;

    const staleIds = staleHubs.map((h) => h.id);

    // 将超时 hub 下的 agent 标记为 offline
    await db
      .update(agents)
      .set({ online: false, hubId: null })
      .where(and(inArray(agents.hubId, staleIds), eq(agents.online, true)));

    // 将超时 hub 标记为 offline
    await db
      .update(hubs)
      .set({ status: "offline", agentCount: 0 })
      .where(inArray(hubs.id, staleIds));

    console.log(`[cron] cleaned up ${staleIds.length} stale hub(s): ${staleIds.join(", ")}`);
  } catch (err) {
    console.error("[cron] stale hub cleanup error:", err);
  }
}

export function startStaleHubCleanup() {
  console.log("[cron] stale hub cleanup started, interval=10s, threshold=30s");
  setInterval(cleanupStaleHubs, CLEANUP_INTERVAL_MS);
}
