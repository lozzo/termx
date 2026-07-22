import { expect, test, type Page, type Route } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const baseURL = process.env.MUXVIA_WEBUX_BASE_URL ?? "http://127.0.0.1:5173";

test.beforeAll(async () => {
  await mkdir("../../../../.artifacts/webux001", { recursive: true });
});

test.beforeEach(async ({ page }) => {
  await installAccountAPI(page);
});

for (const viewport of [
  { name: "mobile", width: 360, height: 780 },
  { name: "tablet", width: 768, height: 900 },
  { name: "desktop", width: 1280, height: 900 },
  { name: "wide", width: 1440, height: 960 },
]) {
  test(`${viewport.name} account shell stays readable at 150 percent`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.goto(`${baseURL}/account`);
    await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
    await expect(page.getByRole("navigation").getByRole("button")).toHaveCount(4);
    await page.evaluate(() => { document.documentElement.style.zoom = "1.5"; });
    await expect(page.getByText("Your devices", { exact: true })).toHaveCount(0);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
    await page.screenshot({ path: `../../../../.artifacts/webux001/${viewport.name}-150.png`, fullPage: true });
  });
}

test("single add-device wizard completes phone activation", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`${baseURL}/account`);
  await page.getByRole("button", { name: "Devices" }).click();
  await page.getByRole("button", { name: "Add device" }).click();
  await expect(page.getByRole("heading", { name: "Add a device" })).toBeVisible();
  await page.getByRole("button", { name: /Phone or tablet/ }).click();
  await page.getByRole("button", { name: "Create QR code" }).click();
  await expect(page.getByText("MXA-ABCD-EFGH-JKMP-QRST-VWXY-234567")).toBeVisible();
  await expect(page.getByText("Pixel 9"), "submitted phone metadata should be reviewable").toBeVisible({ timeout: 5_000 });
  await page.getByRole("button", { name: "Approve phone" }).click();
  await expect(page.getByText(/finishing activation/i)).toBeVisible();
  await page.getByRole("button", { name: "Done" }).click();
  await expect(page.getByText("Pixel 9")).toBeVisible();
});

test("single add-device wizard completes daemon enrollment and protects removal", async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto(`${baseURL}/account`);
  await page.getByRole("button", { name: "Devices" }).click();
  await page.getByRole("button", { name: "Add device" }).click();
  await page.getByRole("button", { name: /Daemon host/ }).click();
  await page.getByRole("button", { name: "Create login code" }).click();
  await expect(page.getByText("MXD-ABCD-EFGH-JKMP-QRST-VWXY-234567", { exact: true })).toBeVisible();
  await expect(page.getByText("Build Mac"), "submitted daemon metadata should be reviewable").toBeVisible({ timeout: 5_000 });
  await page.getByRole("button", { name: "Approve daemon" }).click();
  await expect(page.getByText(/completing its identity proof/i)).toBeVisible();
  await page.getByRole("button", { name: "Done" }).click();
  await page.getByTestId("account-device-daemon-1").getByRole("button", { name: "Remove daemon" }).click();
  await expect(page.getByRole("heading", { name: "Confirm this action" })).toBeVisible();
  await page.getByLabel("Current password").fill("correct horse battery staple");
  await page.getByRole("button", { name: "Confirm" }).click();
  await expect(page.getByText("Build Mac")).toHaveCount(0);
});

test("advanced topology stays outside primary navigation and keyboard reachable", async ({ page }) => {
  await page.goto(`${baseURL}/account`);
  await page.getByRole("button", { name: "Devices" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("button", { name: /Advanced connections/ })).toBeVisible();
  await expect(page.getByRole("navigation").getByText("Advanced")).toHaveCount(0);
  await page.getByRole("button", { name: /Advanced connections/ }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByText("Signaling control relations")).toBeVisible();
  await expect(page.getByText("Command outbox")).toBeVisible();
});

test("simplified Chinese keeps the mobile device flow reachable", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 780 });
  await page.goto(`${baseURL}/account`);
  await page.getByLabel("Language").selectOption("zh-CN");
  await expect(page.getByRole("heading", { name: "概览" })).toBeVisible();
  await page.getByRole("button", { name: "设备" }).click();
  await page.getByRole("button", { name: "添加设备" }).click();
  await expect(page.getByRole("heading", { name: "添加设备" })).toBeVisible();
  await expect(page.getByRole("button", { name: /手机或平板/ })).toBeVisible();
  await expect(page.getByRole("button", { name: /守护进程主机/ })).toBeVisible();
});

async function installAccountAPI(page: Page) {
  let phoneInspect = 0;
  let daemonInspect = 0;
  let devices = [
    device("daemon-1", "Studio Mac", "MANAGED_DEVICE_KIND_DAEMON", true),
    device("phone-1", "Pixel 9", "MANAGED_DEVICE_KIND_CLIENT", false),
  ];
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    switch (path) {
      case "/api/v1/account/commerce":
        return json(route, { account: { accountId: "account-1", displayName: "Ada", email: "ada@example.com", authRevision: "4" }, subscription: { planId: "pro", status: "SUBSCRIPTION_STATUS_ACTIVE" }, entitlement: { capability: { managedP2pMaxConcurrency: 4, relay: { maxBytesPerPeriod: "10737418240" } } }, orders: [], paymentEvents: [], audit: [{ auditId: "audit-1", action: "account.login", occurredAtUnixMillis: "1784764800000" }] });
      case "/api/v1/management/relay/quota":
        return json(route, { period: { remainingBytes: "8589934592" } });
      case "/api/v1/management/devices/list":
        return json(route, { devices });
      case "/api/v1/management/topology/list":
        return json(route, { presences: [{ daemonDeviceId: "daemon-1", controlOwnerHubId: "hub-us", assignmentEpoch: "2", presenceSessionId: "presence-1", availability: "AVAILABILITY_ONLINE", freshness: "FRESHNESS_FRESH" }], peerSessions: [] });
      case "/api/v1/management/commands/list":
        return json(route, { commands: [] });
      case "/api/v1/mobile-activations/create":
        return json(route, phoneActivation("MOBILE_ACTIVATION_STATE_WAITING_FOR_DEVICE"));
      case "/api/v1/mobile-activations/inspect":
        phoneInspect += 1;
        return json(route, phoneActivation(phoneInspect > 0 ? "MOBILE_ACTIVATION_STATE_WAITING_FOR_APPROVAL" : "MOBILE_ACTIVATION_STATE_WAITING_FOR_DEVICE", { displayName: "Pixel 9", platform: "Android", muxviaVersion: "0.1.0" }));
      case "/api/v1/mobile-activations/approve":
        return json(route, {});
      case "/api/v1/daemon-enrollments/create":
        return json(route, daemonEnrollment("DAEMON_ENROLLMENT_STATE_WAITING_FOR_DEVICE"));
      case "/api/v1/daemon-enrollments/inspect":
        daemonInspect += 1;
        return json(route, daemonEnrollment(daemonInspect > 0 ? "DAEMON_ENROLLMENT_STATE_WAITING_FOR_APPROVAL" : "DAEMON_ENROLLMENT_STATE_WAITING_FOR_DEVICE", { displayName: "Build Mac", hostname: "build.local", platform: "darwin", muxviaVersion: "0.1.0" }));
      case "/api/v1/daemon-enrollments/approve":
        devices = devices.filter((item) => item.deviceId !== "daemon-1").concat(device("daemon-1", "Build Mac", "MANAGED_DEVICE_KIND_DAEMON", true));
        return json(route, {});
      case "/api/v1/management/reauth":
        return json(route, {});
      case "/api/v1/management/commands": {
        const body = route.request().postDataJSON() as { target?: { cloud_device?: { device_id?: string } } };
        const id = body.target?.cloud_device?.device_id;
        if (id) devices = devices.filter((item) => item.deviceId !== id);
        return json(route, {});
      }
      case "/api/v1/account/logout":
      case "/api/v1/checkout":
      case "/api/v1/checkout/test-payment":
      case "/api/v1/subscription/transition":
        return json(route, {});
      default:
        return json(route, {});
    }
  });
}

function device(deviceId: string, displayName: string, deviceKind: string, online: boolean) {
  return { deviceId, displayName, deviceKind, authEpoch: "1", presence: online ? { availability: "AVAILABILITY_ONLINE", freshness: "FRESHNESS_FRESH" } : undefined };
}

function phoneActivation(state: string, clientMetadata?: Record<string, string>) {
  return { userCode: "MXA-ABCD-EFGH-JKMP-QRST-VWXY-234567", qrPayload: "muxvia-cloud-activate:v1:test", state, expiresAtUnix: "1784765400", clientMetadata };
}

function daemonEnrollment(state: string, daemonMetadata?: Record<string, string>) {
  return { userCode: "MXD-ABCD-EFGH-JKMP-QRST-VWXY-234567", state, expiresAtUnix: "1784765400", daemonDeviceId: "daemon-1", daemonMetadata };
}

function json(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}
