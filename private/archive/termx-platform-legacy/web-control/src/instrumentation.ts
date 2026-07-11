export async function register() {
  // 仅在 Node.js 服务端运行定时任务（排除 Edge runtime）
  if (process.env.NEXT_RUNTIME === "nodejs") {
    const { startStaleHubCleanup } = await import("@/lib/cron/stale-hub-cleanup");
    startStaleHubCleanup();
  }
}
