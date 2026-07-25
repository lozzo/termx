import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";

test.beforeEach(async ({ page }) => page.addInitScript(() => localStorage.setItem("muxvia-operator-language", "en")));

const publicURL = process.env.MUXVIA_PUBLIC_URL;
const accountEmail = process.env.MUXVIA_ACCOUNT_EMAIL;
const accountPassword = process.env.MUXVIA_ACCOUNT_PASSWORD;

test.beforeAll(async () => {
  if (!publicURL || !accountEmail || !accountPassword) throw new Error("OPSCOM001 public URL and account credentials are required");
  await mkdir("../../../../.artifacts/opscom001", { recursive: true });
});

test("operator commerce UI publishes promotion, adjusts subscription, and observes discounted payment", async ({ page }) => {
  const promotionCode = `E2E${Date.now().toString().slice(-6)}`;
  await page.setViewportSize({ width: 1440, height: 960 });
  await loginOperator(page);
  await expect(page.getByTestId("operator-commerce-operations")).toBeVisible();

  await page.getByTestId("catalog-advanced-editor").locator("summary").click();
  const editor = page.getByTestId("catalog-editor");
  const contract = JSON.parse(await editor.inputValue()) as any;
  contract.catalogVersion = (BigInt(contract.catalogVersion) + 1n).toString();
  for (const plan of contract.plans) plan.planVersion = (BigInt(plan.planVersion) + 1n).toString();
  const pro = contract.plans.find((plan: any) => plan.planId === "pro");
  pro.price = { mode: "CATALOG_PRICE_MODE_CONFIGURED", currency: "USD", monthlyMinor: "1000", yearlyMinor: "10000", label: "$10 / month" };
  pro.creem = { monthlyProductId: "prod_opscom_e2e_monthly", yearlyProductId: "prod_opscom_e2e_yearly" };
  await editor.fill(JSON.stringify(contract, null, 2));
  await page.getByTestId("catalog-reason").fill("Enable OPSCOM checkout fixture");
  await page.getByTestId("catalog-publish").click();
  await expect(page.getByText(`Catalog ${contract.catalogVersion}`, { exact: true })).toBeVisible();

  await page.goto(`${publicURL}/operator/promotions`);
  const until = new Date(Date.now() + 60 * 60 * 1000);
  const localUntil = new Date(until.getTime() - until.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
  await page.getByTestId("promotion-code").fill(promotionCode);
  await page.getByLabel("Value").fill("50");
  await page.getByLabel("Total limit").fill("1");
  await page.getByLabel("Effective until").fill(localUntil);
  await page.getByLabel("Creem discount code").fill(`disc_${promotionCode.toLowerCase()}`);
  await page.getByTestId("operator-promotions").getByLabel("Publish reason").fill("OPSCOM browser verification");
  await page.getByTestId("promotion-create").click();
  await expect(page.getByTestId("operator-promotions").getByText(promotionCode, { exact: true })).toBeVisible();

  await page.goto(`${publicURL}/operator/subscriptions`);
  await page.getByTestId("operator-subscriptions").locator("button").first().click();
  await page.getByLabel("Adjustment").selectOption("2");
  await page.getByTestId("adjustment-reason").fill("Seven day support extension");
  await page.getByTestId("adjustment-create").click();
  await expect(page.getByTestId("operator-subscription-adjustment").getByText("Seven day support extension", { exact: false })).toBeVisible();

  await page.goto(`${publicURL}/login`);
  await page.getByTestId("account-email").fill(accountEmail!);
  await page.getByTestId("account-password").fill(accountPassword!);
  await page.getByTestId("account-submit").click();
  await expect(page).toHaveURL(/\/account/);
  await page.getByRole("button", { name: "Plans" }).click();
  await page.getByTestId("checkout-promotion-code").fill(promotionCode);
  await page.getByRole("button", { name: "Activate Pro with test provider" }).click();
  await expect(page.getByText("pro", { exact: true }).first()).toBeVisible();

  await page.goto(`${publicURL}/operator/orders`);
  await expect(page.getByTestId("operator-orders").getByText("$5.00", { exact: false })).toBeVisible();
  await expect(page.getByTestId("operator-orders").getByText("PAID", { exact: true })).toBeVisible();
  await page.getByText("Event timeline", { exact: true }).click();
  await expect(page.getByTestId("operator-orders").getByText("SUCCEEDED", { exact: true })).toBeVisible();
  await page.screenshot({ path: "../../../../.artifacts/opscom001/operator-desktop.png", fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await page.screenshot({ path: "../../../../.artifacts/opscom001/operator-mobile.png", fullPage: true });
});

async function loginOperator(page: import("@playwright/test").Page) {
  await page.goto(`${publicURL}/login`);
  await page.getByTestId("account-email").fill(accountEmail!);
  await page.getByTestId("account-password").fill(accountPassword!);
  await page.getByTestId("account-submit").click();
  await expect(page).toHaveURL(/\/account/);
  await page.goto(`${publicURL}/operator/plans`);
  await page.getByRole("button", { name: "Verify identity" }).click();
  await page.getByTestId("operator-reauth").getByLabel("Account password").fill(accountPassword!);
  await page.getByRole("button", { name: "Unlock changes" }).click();
  await expect(page.getByText("CHANGES UNLOCKED")).toBeVisible();
}
