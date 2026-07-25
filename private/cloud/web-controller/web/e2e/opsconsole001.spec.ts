import { expect, test, type Page, type Route } from "@playwright/test";

const baseURL = process.env.MUXVIA_OPSCONSOLE_E2E_BASE_URL ?? "http://127.0.0.1:5173";

test("真实子路由只加载当前模块并支持浏览器历史", async ({ page }) => {
  const requests: string[] = [];
  await installOperatorAPI(page, requests);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${baseURL}/operator`);

  await expect(page).toHaveURL(/\/operator\/users$/);
  const navigation = page.getByRole("navigation", { name: "运营管理模块" });
  await expect(navigation).toBeVisible();
  await expect(navigation.getByRole("link")).toHaveCount(9);
  await expect(page.getByRole("heading", { level: 1, name: "用户管理" })).toBeVisible();
  await expect(page.getByTestId("operator-orders")).toHaveCount(0);
  await expect(page.getByTestId("operator-releases")).toHaveCount(0);
  expect(requests.filter((path) => path.endsWith("/list"))).toEqual(["/api/v1/operator/accounts/list"]);

  await navigation.getByRole("link", { name: "订单管理" }).click();
  await expect(page).toHaveURL(/\/operator\/orders$/);
  await expect(page.getByRole("heading", { level: 1, name: "订单管理" })).toBeVisible();
  await expect(page.getByTestId("operator-orders")).toBeVisible();
  await expect(page.getByTestId("operator-releases")).toHaveCount(0);
  expect(requests.filter((path) => path.endsWith("/list"))).toEqual([
    "/api/v1/operator/accounts/list",
    "/api/v1/operator/orders/list",
  ]);

  await navigation.getByRole("link", { name: "Hub 管理" }).click();
  await expect(page).toHaveURL(/\/operator\/hubs$/);
  await expect(page.getByRole("heading", { level: 1, name: "Hub 管理" })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/\/operator\/orders$/);
  await expect(page.getByRole("status")).toHaveCount(0);
  await expect(page.getByTestId("operator-orders")).toBeVisible();
});

test("移动端使用抽屉且深链接刷新保持当前模块", async ({ page }) => {
  await installOperatorAPI(page, []);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${baseURL}/operator/releases`);

  await expect(page.getByRole("heading", { level: 1, name: "版本管理" })).toBeVisible();
  await expect(page.getByTestId("operator-releases")).toBeVisible();
  await page.getByRole("button", { name: "打开管理菜单" }).click();
  await expect(page.getByRole("navigation", { name: "运营管理模块" })).toBeVisible();
  await expect(page.getByRole("button", { name: "打开管理菜单" })).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("link", { name: "版本管理" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("button", { name: "打开管理菜单" })).toHaveAttribute("aria-expanded", "false");
  await expect(page.getByRole("button", { name: "打开管理菜单" })).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
});

test("768px 与 150% 缩放下没有页面级横向溢出", async ({ page }) => {
  await installOperatorAPI(page, []);
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto(`${baseURL}/operator/hubs`);
  await page.evaluate(() => { document.documentElement.style.zoom = "1.5"; });
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
  await expect(page.getByRole("heading", { level: 1, name: "Hub 管理" })).toBeVisible();
});

test("套餐使用结构化表单并为历史版本建立深链接", async ({ page }) => {
  await installOperatorAPI(page, [], operatorModules(), { catalogReleases: [catalogRelease()] });
  await page.goto(`${baseURL}/operator/plans`);

  const editor = page.getByTestId("catalog-structured-editor");
  await expect(editor).toBeVisible();
  await editor.getByLabel("新目录版本").fill("2");
  await editor.getByLabel("月付金额（最小货币单位）").fill("1200");
  await page.getByTestId("catalog-advanced-editor").locator("summary").click();
  await expect(page.getByTestId("catalog-editor")).toHaveValue(/"catalogVersion": "2"/);
  await expect(page.getByTestId("catalog-editor")).toHaveValue(/"monthlyMinor": "1200"/);
  await page.getByRole("button", { name: "目录版本 1" }).click();
  await expect(page).toHaveURL(/\/operator\/catalog\/1$/);
  await expect(page.getByTestId("catalog-editor")).toHaveValue(/"catalogVersion": "1"/);
});

test("订阅详情独立加载且不跨模块请求用户特权", async ({ page }) => {
  const requests: string[] = [];
  await installOperatorAPI(page, requests, operatorModules(), {
    catalogReleases: [catalogRelease()],
    subscriptions: [{ subscriptionId: "subscription-1", accountId: "account-1", planId: "pro", planVersion: "1", revision: "3", status: "SUBSCRIPTION_STATUS_ACTIVE" }],
    accountDetail: operatorAccountDetail(),
  });
  await page.goto(`${baseURL}/operator/subscriptions`);

  await page.getByTestId("operator-subscriptions").getByRole("button", { name: /pro v1/ }).click();
  await expect(page).toHaveURL(/\/operator\/subscriptions\/subscription-1$/);
  await expect(page.getByTestId("operator-subscriptions").getByTestId("operator-subscription-adjustment")).toBeVisible();
  expect(requests).not.toContain("/api/v1/operator/entitlement-overrides/list");
});

test("优惠码详情展示兑换记录并提交停用原因", async ({ page }) => {
  const requests: string[] = [];
  await installOperatorAPI(page, requests, operatorModules(), {
    promotions: [{ promotionId: "promotion-1", code: "OPS20", discountKind: "PROMOTION_DISCOUNT_KIND_PERCENT", percentBasisPoints: 2000, planIds: ["pro"], maxRedemptions: 10, creemDiscountCode: "disc_ops20", state: "PROMOTION_STATE_ACTIVE", revision: "2" }],
    redemptions: [{ redemptionId: "redemption-1", promotionId: "promotion-1", accountId: "account-1", orderId: "order-1", state: "PROMOTION_REDEMPTION_STATE_REDEEMED", discountMinor: "200" }],
  });
  await page.goto(`${baseURL}/operator/promotions`);
  await unlockOperator(page);

  await page.getByRole("link", { name: "OPS20" }).click();
  await expect(page).toHaveURL(/\/operator\/promotions\/promotion-1$/);
  await expect(page.getByTestId("promotion-detail")).toContainText("account-1 · REDEEMED");
  await page.getByLabel("停用原因").fill("活动已经结束");
  await page.getByRole("button", { name: "停用优惠码" }).click();
  expect(requests).toContain("/api/v1/operator/promotions/disable");
});

test("用户特权详情通过后端接口撤销有效 override", async ({ page }) => {
  const requests: string[] = [];
  await installOperatorAPI(page, requests, operatorModules(), {
    accounts: [{ account: { accountId: "account-1", email: "owner@example.test" }, subscription: { planId: "pro", status: "SUBSCRIPTION_STATUS_ACTIVE" }, relayQuota: {} }],
    accountDetail: operatorAccountDetail(),
    overrides: [{ overrideId: "override-1", accountId: "account-1", capabilityMask: "cloudDeviceLimit", capability: { cloudDeviceLimit: 8 }, effectiveUntilUnixMillis: String(Date.now() + 3600_000), reason: "临时支持", revision: "2" }],
  });
  await page.goto(`${baseURL}/operator/privileges`);
  await unlockOperator(page);

  await page.getByTestId("operator-account-account-1").click();
  await expect(page).toHaveURL(/\/operator\/privileges\/account-1$/);
  await page.getByLabel("特权 override-1 的撤销原因").fill("支持工单已结束");
  await page.getByRole("button", { name: "撤销特权" }).click();
  expect(requests).toContain("/api/v1/operator/entitlement-overrides/revoke");
});

test("侧栏只展示后端授权的模块", async ({ page }) => {
  await installOperatorAPI(page, [], ["OPERATOR_WORKSPACE_MODULE_ORDERS", "OPERATOR_WORKSPACE_MODULE_HUBS"]);
  await page.goto(`${baseURL}/operator/releases`);

  await expect(page).toHaveURL(/\/operator\/orders$/);
  const navigation = page.getByRole("navigation", { name: "运营管理模块" });
  await expect(navigation.getByRole("link")).toHaveCount(2);
  await expect(navigation.getByRole("link", { name: "订单管理" })).toBeVisible();
  await expect(navigation.getByRole("link", { name: "Hub 管理" })).toBeVisible();
});

async function unlockOperator(page: Page) {
  await page.getByRole("button", { name: "验证身份" }).click();
  await page.getByTestId("operator-reauth").getByLabel("账号密码").fill("account-password");
  await page.getByRole("button", { name: "解锁变更操作" }).click();
}

type OperatorFixtures = {
  catalogReleases?: unknown[];
  subscriptions?: unknown[];
  accountDetail?: unknown;
  promotions?: unknown[];
  redemptions?: unknown[];
  accounts?: unknown[];
  overrides?: unknown[];
};

async function installOperatorAPI(page: Page, requests: string[], modules = operatorModules(), fixtures: OperatorFixtures = {}) {
  await page.route("**/api/v1/operator/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    requests.push(path);
    if (path === "/api/v1/operator/workspace") return json(route, { modules });
    if (path === "/api/v1/operator/reauth") return json(route, { expiresAtUnixMillis: String(Date.now() + 300_000) });
    if (path === "/api/v1/operator/accounts/list") return json(route, { accounts: fixtures.accounts ?? [], page: {} });
    if (path === "/api/v1/operator/agents/list") return json(route, { agents: [], page: {} });
    if (path === "/api/v1/operator/orders/list") return json(route, { orders: [], page: {} });
    if (path === "/api/v1/operator/subscriptions/list") return json(route, { subscriptions: fixtures.subscriptions ?? [], page: {} });
    if (path === "/api/v1/operator/catalog/list") return json(route, { releases: fixtures.catalogReleases ?? [], page: {} });
    if (path === "/api/v1/operator/accounts/get") return json(route, fixtures.accountDetail ?? {});
    if (path === "/api/v1/operator/entitlement-overrides/list") return json(route, { overrides: fixtures.overrides ?? [], page: {} });
    if (path === "/api/v1/operator/entitlement-overrides/revoke") return json(route, { override: fixtures.overrides?.[0] ?? {} });
    if (path === "/api/v1/operator/promotions/list") return json(route, { promotions: fixtures.promotions ?? [], page: {} });
    if (path === "/api/v1/operator/promotions/redemptions") return json(route, { redemptions: fixtures.redemptions ?? [], page: {} });
    if (path === "/api/v1/operator/promotions/disable") return json(route, { promotion: fixtures.promotions?.[0] ?? {} });
    if (path === "/api/v1/operator/fleet/list") return json(route, { hubs: [], page: {} });
    if (path === "/api/v1/operator/releases/list") return json(route, { artifacts: [], channels: [], operatorAudit: [], page: {} });
    return json(route, {});
  });
}

function catalogRelease() {
  return {
    active: true,
    catalog: {
      catalogVersion: "1",
      plans: [{
        planId: "pro", planVersion: "1", billingPeriodDays: 30,
        capability: { managedP2pEnabled: true, managedP2pMaxConcurrency: 2, cloudDeviceLimit: 4, relay: { maxBytesPerPeriod: "1048576" } },
        price: { mode: "CATALOG_PRICE_MODE_CONFIGURED", currency: "USD", monthlyMinor: "1000", yearlyMinor: "10000", label: "$10 / month" },
        presentation: { name: "Pro" },
      }],
    },
    publishedAtUnixMillis: "1784973600000",
    revision: "1",
  };
}

function operatorAccountDetail() {
  return {
    commerce: {
      account: { accountId: "account-1", email: "owner@example.test" },
      subscription: { subscriptionId: "subscription-1", accountId: "account-1", planId: "pro", planVersion: "1", revision: "3", status: "SUBSCRIPTION_STATUS_ACTIVE" },
      subscriptionAdjustments: [], audit: [],
    },
    devices: { devices: [], page: {} }, topology: { presences: [], peerSessions: [], page: {} }, sessions: [], commands: [], operatorAudit: [],
  };
}

function operatorModules() {
  return [
    "OPERATOR_WORKSPACE_MODULE_USERS", "OPERATOR_WORKSPACE_MODULE_AGENTS",
    "OPERATOR_WORKSPACE_MODULE_ORDERS", "OPERATOR_WORKSPACE_MODULE_SUBSCRIPTIONS",
    "OPERATOR_WORKSPACE_MODULE_PLANS", "OPERATOR_WORKSPACE_MODULE_PRIVILEGES",
    "OPERATOR_WORKSPACE_MODULE_PROMOTIONS", "OPERATOR_WORKSPACE_MODULE_HUBS",
    "OPERATOR_WORKSPACE_MODULE_RELEASES",
  ];
}

function json(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}
