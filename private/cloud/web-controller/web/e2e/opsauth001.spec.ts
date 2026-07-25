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
  await page.addInitScript(() => localStorage.setItem("muxvia-language", "en"));
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${baseURL}/account`);

  const management = page.getByRole("navigation", { name: "Operations modules" });
  await expect(management).toBeVisible();
  await expect(management.getByRole("link")).toHaveCount(9);
  await expect(page.getByText(/isAdmin|readonly|admin role/i)).toHaveCount(0);
  await page.evaluate(() => { document.body.dataset.consoleShell = "persistent"; });
  await management.getByRole("link", { name: "Users" }).click();
  await expect(page).toHaveURL(/\/operator\/users$/);
  await expect.poll(() => page.evaluate(() => document.body.dataset.consoleShell)).toBe("persistent");
  await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
  await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "运营管理模块" }).getByRole("link")).toHaveCount(9);
  await expect(page.getByRole("heading", { level: 1, name: "用户管理" })).toBeVisible();
  await expect(page.getByText("浏览模式：变更操作需要确认身份")).toBeVisible();
  await page.getByRole("button", { name: "验证身份" }).click();
  await expect(page.getByRole("dialog", { name: "确认本次管理操作" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "确认本次管理操作" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "验证身份" })).toBeFocused();
  await page.getByRole("button", { name: "验证身份" }).click();
  await page.getByTestId("operator-reauth").getByLabel("账号密码").fill("account-password");
  await page.getByRole("button", { name: "解锁变更操作" }).click();
  await expect(page.getByText("变更操作已解锁")).toBeVisible();
  await page.screenshot({ path: "test-results/operator-zh-CN.png", fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${baseURL}/account`);
  await page.getByRole("button", { name: "Open management menu" }).click();
  await expect(page.getByRole("navigation", { name: "Operations modules" }).getByRole("link")).toHaveCount(9);
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
