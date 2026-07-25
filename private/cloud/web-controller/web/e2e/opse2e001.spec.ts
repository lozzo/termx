import { expect, test } from "@playwright/test";
import { mkdir, readFile } from "node:fs/promises";

type SupervisorManifest = {
  controller: { operator_url: string; public_url: string };
  credentials_path: string;
};

type DevelopmentCredentials = {
  public_url: string;
  account_email: string;
  account_password: string;
  operator_url: string;
  release_artifact_json: string;
};

test.beforeAll(async () => {
  await mkdir("../../../../.artifacts/opse2e001", { recursive: true });
});

test("operator completes all nine modules against Controller, PostgreSQL, and two Edges", async ({ page }) => {
  const manifestPath = process.env.MUXVIA_OPSE2E_MANIFEST;
  if (!manifestPath) throw new Error("MUXVIA_OPSE2E_MANIFEST is required");
  const manifest = JSON.parse(await readFile(manifestPath, "utf8")) as SupervisorManifest;
  const credentials = JSON.parse(await readFile(manifest.credentials_path, "utf8")) as DevelopmentCredentials;
  const promotionCode = `OPS${Date.now().toString().slice(-7)}`;

  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${credentials.public_url}/login`);
  await page.getByTestId("account-email").fill(credentials.account_email);
  await page.getByTestId("account-password").fill(credentials.account_password);
  await page.getByTestId("account-submit").click();
  await expect(page).toHaveURL(/\/account/);
  await page.goto(`${credentials.public_url}/operator`);
  await page.getByTestId("operator-reauth").getByLabel(/Confirm account password/).fill(credentials.account_password);
  await page.getByRole("button", { name: "Unlock changes" }).click();
  await expect(page.getByText("CHANGES UNLOCKED")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Control plane" })).toBeVisible();
  await expect(page.getByTestId("hub-hub-edge-a")).toContainText("Ready");
  await expect(page.getByTestId("hub-hub-edge-b")).toContainText("Ready");

  const catalogEditor = page.getByTestId("catalog-editor");
  const catalog = JSON.parse(await catalogEditor.inputValue()) as any;
  catalog.catalogVersion = (BigInt(catalog.catalogVersion) + 1n).toString();
  for (const plan of catalog.plans) plan.planVersion = (BigInt(plan.planVersion) + 1n).toString();
  const pro = catalog.plans.find((plan: any) => plan.planId === "pro");
  pro.price = { mode: "CATALOG_PRICE_MODE_CONFIGURED", currency: "USD", monthlyMinor: "1000", yearlyMinor: "10000", label: "$10 / month" };
  pro.creem = { monthlyProductId: "prod_opse2e_monthly", yearlyProductId: "prod_opse2e_yearly" };
  await catalogEditor.fill(JSON.stringify(catalog, null, 2));
  await page.getByTestId("catalog-reason").fill("OPSE2E catalog release");
  await page.getByTestId("catalog-publish").click();
  await expect(page.getByText(`Catalog ${catalog.catalogVersion}`, { exact: true })).toBeVisible();

  const until = new Date(Date.now() + 60 * 60 * 1000);
  const localUntil = new Date(until.getTime() - until.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
  const promotion = page.getByTestId("operator-promotions");
  await promotion.getByTestId("promotion-code").fill(promotionCode);
  await promotion.getByLabel("Value").fill("50");
  await promotion.getByLabel("Total limit").fill("1");
  await promotion.getByLabel("Effective until").fill(localUntil);
  await promotion.getByLabel("Creem discount code").fill(`disc_${promotionCode.toLowerCase()}`);
  await promotion.getByLabel("Publish reason").fill("OPSE2E promotion release");
  await promotion.getByTestId("promotion-create").click();
  await expect(promotion.getByText(promotionCode, { exact: true })).toBeVisible();

  await page.locator('[data-testid^="operator-account-"]').first().click();
  await page.getByTestId("operator-subscription-adjustment").getByLabel("Adjustment").selectOption("2");
  await page.getByTestId("adjustment-reason").fill("OPSE2E subscription extension");
  await page.getByTestId("adjustment-create").click();
  await expect(page.getByTestId("operator-subscription-adjustment")).toContainText("OPSE2E subscription extension");
  await page.getByTestId("override-value").fill("6");
  await page.getByTestId("override-until").fill(localUntil);
  await page.getByTestId("override-reason").fill("OPSE2E temporary privilege");
  await page.getByTestId("override-create").click();
  await expect(page.getByTestId("operator-overrides")).toContainText("cloud_device_limit");
  await expect(page.getByTestId("operator-overrides")).toContainText("ACTIVE");

  await page.goto(`${credentials.public_url}/account`);
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  await page.getByRole("button", { name: "Plans" }).click();
  await page.getByTestId("checkout-promotion-code").fill(promotionCode);
  await page.getByRole("button", { name: "Upgrade to Pro" }).click();
  await expect(page.getByText("pro", { exact: true }).first()).toBeVisible();

  await page.goto(`${credentials.public_url}/operator`);
  await expect(page.getByTestId("operator-orders")).toContainText("$5.00");
  await expect(page.getByTestId("operator-orders").getByText("PAID", { exact: true })).toBeVisible();
  await expect(page.getByTestId("operator-subscriptions").getByText("ACTIVE", { exact: true })).toBeVisible();
  await page.locator('[data-testid^="operator-account-"]').first().click();
  await page.getByTestId("operator-suspend").click();
  await expect(page.getByText("SUSPENDED", { exact: true }).first()).toBeVisible();
  await page.getByTestId("operator-restore").click();
  await expect(page.getByText("ACTIVE", { exact: true }).first()).toBeVisible();

  await page.getByTestId("release-draft").fill(credentials.release_artifact_json);
  await page.getByTestId("release-reason").fill("OPSE2E signed Android release");
  await page.getByTestId("release-publish").click();
  const release = page.getByTestId("release-android-opse2e001-v1");
  await expect(release).toBeVisible();
  await release.getByRole("button", { name: "Activate" }).click();
  await expect(release).toContainText("ACTIVE");

  await page.getByText("Add Hub deployment").click();
  const hubPanel = page.getByTestId("hub-create-panel");
  await hubPanel.getByLabel("Hub ID").fill("hub-opse2e-pending");
  await hubPanel.getByLabel("Edge deployment ID").fill("edge-opse2e-pending");
  await hubPanel.getByLabel("Relay ID").fill("relay-opse2e-pending");
  await hubPanel.getByLabel("Region").fill("local-pending");
  await hubPanel.getByLabel("Public label").fill("OPSE2E pending region");
  await hubPanel.getByLabel("Public Hub URL").fill("https://pending.edge.muxvia.test");
  await hubPanel.getByLabel("Health URL").fill("https://pending.edge.muxvia.test/healthz");
  await hubPanel.getByLabel("Capacity").fill("20");
  await hubPanel.getByLabel("Hub control public key").fill(base64Key(3));
  await hubPanel.getByLabel("Relay control public key").fill(base64Key(4));
  await hubPanel.getByLabel("Change reason").fill("OPSE2E Hub directory mutation");
  await hubPanel.getByRole("button", { name: "Create pending deployment" }).click();
  await expect(page.getByTestId("hub-hub-opse2e-pending")).toContainText("Pending approval");

  const csrfStatus = await page.evaluate(async () => {
    const response = await fetch("/api/v1/operator/releases/channel", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    return response.status;
  });
  expect(csrfStatus).toBe(401);

  await page.getByTestId("directory-agents").click();
  const agent = page.getByTestId("operator-agent-daemon-edge-a");
  await expect(agent).toContainText("ONLINE");
  const kickResponse = page.waitForResponse((response) => response.url().endsWith("/api/v1/operator/commands") && response.request().method() === "POST");
  await agent.getByTestId("kick-daemon-edge-a").click();
  expect((await kickResponse).status()).toBe(202);
  await page.getByTestId("directory-users").click();
  const account = page.locator('[data-testid^="operator-account-"]').first();
  await expect.poll(async () => {
    await account.click();
    return await page.getByTestId("operator-command-results").textContent();
  }, { timeout: 10_000 }).toContain("EXECUTION APPLIED");

  await page.screenshot({ path: "../../../../.artifacts/opse2e001/operator-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  const mobileLayout = await page.evaluate(() => ({
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    offenders: Array.from(document.querySelectorAll("body *")).map((element) => {
      const box = element.getBoundingClientRect();
      return { tag: element.tagName, testId: element.getAttribute("data-testid"), text: element.textContent?.trim().slice(0, 80), left: Math.round(box.left), right: Math.round(box.right), width: Math.round(box.width) };
    }).filter((item) => item.right > document.documentElement.clientWidth + 1 || item.left < -1).slice(0, 12),
  }));
  expect(mobileLayout.overflow, JSON.stringify(mobileLayout.offenders)).toBeLessThanOrEqual(1);
  await page.screenshot({ path: "../../../../.artifacts/opse2e001/operator-mobile.png", fullPage: true });
});

function base64Key(value: number) {
  return btoa(String.fromCharCode(...new Uint8Array(32).fill(value)));
}
