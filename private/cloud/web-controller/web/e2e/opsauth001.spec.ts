import { expect, test, type Page, type Route } from "@playwright/test";

const baseURL = process.env.MUXVIA_OPSAUTH_E2E_BASE_URL ?? "http://127.0.0.1:5173";

test("ordinary account has no management projection", async ({ page }) => {
  await installAccountAPI(page, false);
  await page.setViewportSize({ width: 390, height: 844 });
  const workspaceResponse = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v1/operator/workspace");
  await page.goto(`${baseURL}/account`);
  expect((await workspaceResponse).status()).toBe(403);
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Management" })).toHaveCount(0);
});

test("administrator uses the account navigation and confirms password for changes", async ({ page }) => {
  await installAccountAPI(page, true);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${baseURL}/account`);

  const management = page.getByRole("navigation", { name: "Management" });
  await expect(management).toBeVisible();
  await expect(management.getByRole("link")).toHaveCount(9);
  await expect(page.getByText(/isAdmin|readonly|admin role/i)).toHaveCount(0);
  await management.getByRole("link", { name: "Releases" }).click();
  await expect(page).toHaveURL(/\/operator#operator-releases$/);
  await expect(page.getByText("READ-ONLY UNTIL CONFIRMED")).toBeVisible();
  await page.getByTestId("operator-reauth").getByLabel(/Confirm account password/).fill("account-password");
  await page.getByRole("button", { name: "Unlock changes" }).click();
  await expect(page.getByText("CHANGES UNLOCKED")).toBeVisible();

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${baseURL}/account`);
  await expect(page.getByRole("navigation", { name: "Management" }).getByRole("link")).toHaveCount(9);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
});

async function installAccountAPI(page: Page, operator: boolean) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/operator/workspace") {
      if (!operator) return route.fulfill({ status: 403, contentType: "application/json", body: "{}" });
      return json(route, operatorWorkspace());
    }
    if (path === "/api/v1/operator/reauth") return json(route, { expiresAtUnixMillis: String(Date.now() + 300_000) });
    if (path === "/api/v1/account/commerce") return json(route, {
      account: { accountId: "account-1", displayName: "Ada", email: "ada@example.test", authRevision: "1" },
      subscription: { planId: "free", status: "SUBSCRIPTION_STATUS_ACTIVE" },
      entitlement: { capability: {} }, orders: [], paymentEvents: [], audit: [],
    });
    if (path === "/api/v1/management/relay/quota") return json(route, { period: { remainingBytes: "0" } });
    if (path === "/api/v1/management/devices/list") return json(route, { devices: [] });
    if (path === "/api/v1/management/topology/list") return json(route, { presences: [], peerSessions: [] });
    if (path === "/api/v1/management/commands/list") return json(route, { commands: [] });
    return json(route, {});
  });
}

function operatorWorkspace() {
  return { modules: [
    "OPERATOR_WORKSPACE_MODULE_USERS", "OPERATOR_WORKSPACE_MODULE_ORDERS",
    "OPERATOR_WORKSPACE_MODULE_SUBSCRIPTIONS", "OPERATOR_WORKSPACE_MODULE_PLANS",
    "OPERATOR_WORKSPACE_MODULE_HUBS", "OPERATOR_WORKSPACE_MODULE_AGENTS",
    "OPERATOR_WORKSPACE_MODULE_RELEASES", "OPERATOR_WORKSPACE_MODULE_PROMOTIONS",
    "OPERATOR_WORKSPACE_MODULE_PRIVILEGES",
  ] };
}

function json(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}
