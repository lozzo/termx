import { expect, test, type Page, type Route } from "@playwright/test";
import { mkdir } from "node:fs/promises";

test.beforeEach(async ({ page }) => page.addInitScript(() => localStorage.setItem("muxvia-operator-language", "en")));

const baseURL = process.env.MUXVIA_OPSUSER_E2E_BASE_URL ?? "http://127.0.0.1:5173";

test.beforeAll(async () => {
  await mkdir("../../../../.artifacts/opsuser001", { recursive: true });
});

test("operator manages users, sessions, and freshness-backed Agents", async ({ page }) => {
  await installOperatorAPI(page);
  await page.setViewportSize({ width: 1366, height: 950 });
  await page.goto(`${baseURL}/operator/agents`);
  await page.getByRole("button", { name: "Verify identity" }).click();
  await page.getByTestId("operator-reauth").getByLabel("Account password").fill("account-password");
  await page.getByRole("button", { name: "Unlock changes" }).click();
  const freshAgent = page.getByTestId("operator-agent-daemon-fresh");
  const staleAgent = page.getByTestId("operator-agent-daemon-stale");
  await expect(freshAgent).toContainText("ONLINE");
  await expect(freshAgent).toContainText("1 active peers");
  await expect(staleAgent).toContainText("STALE");
  await expect(freshAgent.getByRole("button", { name: "Kick" })).toBeVisible();
  await expect(staleAgent.getByRole("button", { name: "Kick" })).toHaveCount(0);
  await Promise.all([
    page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v1/operator/commands"),
    freshAgent.getByRole("button", { name: "Kick" }).click(),
  ]);
  await freshAgent.locator("button").first().click();
  await expect(page.getByTestId("operator-command-command-kick")).toContainText("AUTH COMMITTED");
  await expect(page.getByTestId("operator-command-command-kick")).toContainText("DELIVERY PENDING");

  await page.getByRole("navigation", { name: "Operations modules" }).getByRole("link", { name: "Users" }).click();
  await page.getByTestId("operator-account-account-1").click();
  const session = page.getByTestId("account-session-session-active");
  await expect(session).toContainText("phone-1");
  page.once("dialog", (dialog) => dialog.accept());
  await session.getByRole("button", { name: "Revoke session" }).click();
  await expect(page.getByTestId("account-session-session-active")).toContainText("REVOKED");
  await expect(page.getByTestId("operator-audit-audit-session-revoke")).toContainText("account.session.revoke");

  await page.screenshot({ path: "../../../../.artifacts/opsuser001/users-agents-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
  await page.getByRole("button", { name: "Open management menu" }).click();
  await page.getByRole("navigation", { name: "Operations modules" }).getByRole("link", { name: "Agents" }).click();
  await expect(page.getByTestId("operator-agent-daemon-fresh")).toBeVisible();
  await expect(page.getByRole("button", { name: "Open management menu" })).toHaveAttribute("aria-expanded", "false");
  await expect.poll(async () => (await page.locator("#console-navigation").boundingBox())?.x ?? 0).toBeLessThan(-250);
  await page.screenshot({ path: "../../../../.artifacts/opsuser001/users-agents-mobile.png", fullPage: true });
});

async function installOperatorAPI(page: Page) {
  let sessionRevoked = false;
  let kickCreated = false;
  await page.route("**/api/v1/operator/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/operator/workspace") return json(route, operatorWorkspace());
    if (path === "/api/v1/operator/reauth") return json(route, { expiresAtUnixMillis: String(Date.now() + 300_000) });
    if (path === "/api/v1/operator/accounts/list") return json(route, accountList());
    if (path === "/api/v1/operator/agents/list") return json(route, agentList());
    if (path === "/api/v1/operator/fleet/list") return json(route, fleetList());
    if (path === "/api/v1/operator/accounts/get") return json(route, accountDetail(sessionRevoked, kickCreated));
    if (path === "/api/v1/operator/accounts/sessions/revoke") {
      sessionRevoked = true;
      return json(route, { sessions: accountDetail(true, kickCreated).sessions });
    }
    if (path === "/api/v1/operator/commands") {
      kickCreated = true;
      return json(route, { command: commandProjection() });
    }
    return json(route, {});
  });
}

function operatorWorkspace() {
  return { modules: ["OPERATOR_WORKSPACE_MODULE_USERS", "OPERATOR_WORKSPACE_MODULE_ORDERS", "OPERATOR_WORKSPACE_MODULE_SUBSCRIPTIONS", "OPERATOR_WORKSPACE_MODULE_PLANS", "OPERATOR_WORKSPACE_MODULE_HUBS", "OPERATOR_WORKSPACE_MODULE_AGENTS", "OPERATOR_WORKSPACE_MODULE_RELEASES", "OPERATOR_WORKSPACE_MODULE_PROMOTIONS", "OPERATOR_WORKSPACE_MODULE_PRIVILEGES"] };
}

function accountList() {
  return {
    accounts: [{
      account: { accountId: "account-1", email: "owner@example.test" },
      subscription: { planId: "pro", status: "SUBSCRIPTION_STATUS_ACTIVE" },
      relayQuota: { usedBytes: "4096" },
    }],
    page: {},
  };
}

function agentList() {
  return {
    agents: [
      agent("daemon-fresh", "Build host", "AVAILABILITY_ONLINE", "FRESHNESS_FRESH", "presence-fresh", "1"),
      agent("daemon-stale", "Old host", "AVAILABILITY_ONLINE", "FRESHNESS_STALE", "presence-stale", "0"),
    ],
    page: {},
  };
}

function agent(deviceId: string, displayName: string, availability: string, freshness: string, presenceSessionId: string, peers: string) {
  return {
    account: { accountId: "account-1", email: "owner@example.test" },
    device: { deviceId, displayName, deviceKind: "MANAGED_DEVICE_KIND_DAEMON", assignedHubId: "hub-us", authEpoch: "3" },
    presence: { daemonDeviceId: deviceId, controlOwnerHubId: "hub-us", assignmentEpoch: "7", presenceSessionId, availability, freshness },
    activePeerSessionCount: peers,
  };
}

function fleetList() {
  return {
    hubs: [
      { deployment: { metadata: { hubId: "hub-us", publicLabel: "US West" } }, hubReady: true },
      { deployment: { metadata: { hubId: "hub-cn", publicLabel: "China East" } }, hubReady: true },
    ],
    page: {},
  };
}

function accountDetail(sessionRevoked: boolean, kickCreated: boolean) {
  return {
    commerce: {
      account: { accountId: "account-1", email: "owner@example.test" },
      subscription: { accountId: "account-1", planId: "pro", status: "SUBSCRIPTION_STATUS_ACTIVE" },
      audit: [],
      subscriptionAdjustments: [],
    },
    devices: { devices: [agentList().agents[0].device], page: {} },
    topology: { presences: [agentList().agents[0].presence], peerSessions: [], page: {} },
    sessions: [{
      sessionId: "session-active",
      clientDeviceId: "phone-1",
      accessExpiresAtUnixMillis: "1784977200000",
      refreshExpiresAtUnixMillis: "1785582000000",
      revision: sessionRevoked ? "2" : "1",
      revoked: sessionRevoked,
    }],
    commands: kickCreated ? [commandProjection()] : [],
    operatorAudit: sessionRevoked ? [{ auditId: "audit-session-revoke", actorId: "operator-1", action: "account.session.revoke", resourceKind: "account_session", resourceId: "session-active", accountId: "account-1", reason: "Operator revoked account session", requestId: "request-session-revoke", beforeRevision: "1", afterRevision: "1", occurredAtUnixMillis: "1784973600000" }] : [],
  };
}

function commandProjection() {
  return {
    commandId: "command-kick",
    accountId: "account-1",
    commandKind: "MANAGEMENT_COMMAND_KIND_KICK_PRESENCE",
    authorityResult: "COMMAND_AUTHORITY_RESULT_COMMITTED",
    deliveryState: "COMMAND_DELIVERY_STATE_PENDING",
    executionState: "COMMAND_EXECUTION_STATE_PENDING",
    observedEffect: "COMMAND_OBSERVED_EFFECT_UNSPECIFIED",
  };
}

async function json(route: Route, body: unknown) {
  await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}
