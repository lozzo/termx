import { expect, test, type Page, type Route } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const baseURL = process.env.MUXVIA_OPSREL_E2E_BASE_URL ?? "http://127.0.0.1:5173";

test.beforeAll(async () => {
  await mkdir("../../../../.artifacts/opsrel001", { recursive: true });
});

test("operator publishes, activates, pauses, and rolls back signed releases", async ({ page }) => {
  await installAPI(page);
  await page.setViewportSize({ width: 1366, height: 950 });
  await page.goto(`${baseURL}/operator`);
  await publish(page, artifact("android-100", "v1.0.0", "100"), "Initial Android release");
  await page.getByTestId("release-android-100").getByRole("button", { name: "Activate" }).click();
  await expect(page.getByTestId("release-android-100")).toContainText("ACTIVE");
  await publish(page, artifact("android-200", "v2.0.0", "200"), "Canary Android release");
  await page.getByTestId("release-android-200").getByRole("button", { name: "Activate" }).click();
  await expect(page.getByTestId("release-android-100").getByRole("button", { name: "Rollback" })).toBeVisible();
  await page.getByTestId("release-android-100").getByRole("button", { name: "Rollback" }).click();
  await expect(page.getByTestId("release-android-100")).toContainText("ACTIVE");
  await page.getByTestId("release-android-100").getByRole("button", { name: "Pause" }).click();
  await expect(page.getByTestId("release-android-100")).toContainText("ACTIVE / PAUSED");
  await page.getByTestId("release-android-100").getByRole("button", { name: "Resume" }).click();
  await expect(page.getByTestId("release-android-100")).not.toContainText("PAUSED");
  await expect(page.getByTestId("release-audit")).toContainText("release.rollback");
  await expect(page.getByTestId("release-audit")).toContainText("release.pause");
  await expect(page.getByTestId("release-audit")).toContainText("release.resume");
  await page.screenshot({ path: "../../../../.artifacts/opsrel001/releases-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
  await page.screenshot({ path: "../../../../.artifacts/opsrel001/releases-mobile.png", fullPage: true });
});

async function publish(page: Page, value: object, reason: string) {
  await page.getByTestId("release-draft").fill(JSON.stringify(value));
  await page.getByTestId("release-reason").fill(reason);
  await page.getByTestId("release-publish").click();
}

function artifact(releaseId: string, version: string, versionCode: string) {
  return { releaseId, product: "RELEASE_PRODUCT_ANDROID", channel: "RELEASE_CHANNEL_STABLE", version, versionCode, os: "android", arch: "arm64", downloadUrl: `https://releases.muxvia.test/${releaseId}.apk`, artifactSize: "4096", sha256: btoa("x".repeat(32)), signingKeyId: "release-key-1", signature: btoa("s".repeat(64)), minCompatibleVersionCode: "1", rolloutBasisPoints: 1000, changelog: version };
}

async function installAPI(page: Page) {
  const artifacts: Record<string, unknown>[] = [];
  const audits: Record<string, unknown>[] = [];
  let channel: Record<string, unknown> | undefined;
  await page.route("**/api/v1/operator/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = route.request().postDataJSON?.() as Record<string, unknown> | undefined;
    if (path === "/api/v1/operator/workspace") return json(route, operatorWorkspace());
    if (path === "/api/v1/operator/reauth") return json(route, { expiresAtUnixMillis: String(Date.now() + 300_000) });
    if (path === "/api/v1/operator/releases/list") return json(route, { artifacts: artifacts.slice().reverse(), channels: channel ? [channel] : [], operatorAudit: audits.slice().reverse(), page: {} });
    if (path === "/api/v1/operator/releases/publish") {
      const value = body?.artifact as Record<string, unknown>;
      artifacts.push({ ...value, publishedAtUnixMillis: "1784973600000" });
      audits.push(audit("release.publish", String(value.release_id), String(body?.reason)));
      return json(route, { artifact: artifacts.at(-1) });
    }
    if (path === "/api/v1/operator/releases/channel") {
      const releaseID = body?.release_id ?? body?.releaseId;
      const release = artifacts.find((item) => (item.releaseId ?? item.release_id) === releaseID);
      if (!release) throw new Error(`release request ${JSON.stringify(body)}`);
      const previousChannel = channel;
      channel = { product: release.product, channel: release.channel, os: release.os, arch: release.arch, activeReleaseId: release.releaseId ?? release.release_id, revision: String(Number(channel?.revision ?? 0) + 1), paused: Boolean(body?.paused), updatedAtUnixMillis: "1784973600000" };
      audits.push(audit(body?.paused ? "release.pause" : previousChannel?.paused && previousChannel.activeReleaseId === releaseID ? "release.resume" : body?.allow_rollback ? "release.rollback" : "release.activate", String(releaseID), String(body?.reason)));
      return json(route, { channel });
    }
    return json(route, {});
  });
}

function operatorWorkspace() {
  return { modules: ["OPERATOR_WORKSPACE_MODULE_USERS", "OPERATOR_WORKSPACE_MODULE_ORDERS", "OPERATOR_WORKSPACE_MODULE_SUBSCRIPTIONS", "OPERATOR_WORKSPACE_MODULE_PLANS", "OPERATOR_WORKSPACE_MODULE_HUBS", "OPERATOR_WORKSPACE_MODULE_AGENTS", "OPERATOR_WORKSPACE_MODULE_RELEASES", "OPERATOR_WORKSPACE_MODULE_PROMOTIONS", "OPERATOR_WORKSPACE_MODULE_PRIVILEGES"] };
}

function audit(action: string, resourceId: string, reason: string) {
  return { auditId: `${action}-${resourceId}-${crypto.randomUUID()}`, actorId: "operator-1", action, resourceKind: action === "release.publish" ? "release_artifact" : "release_channel", resourceId, reason, beforeRevision: "0", afterRevision: "1", occurredAtUnixMillis: "1784973600000" };
}

async function json(route: Route, body: unknown) {
  await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}
