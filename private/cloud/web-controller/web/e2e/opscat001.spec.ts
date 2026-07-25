import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const publicURL = process.env.MUXVIA_PUBLIC_URL;
const accountEmail = process.env.MUXVIA_ACCOUNT_EMAIL;
const accountPassword = process.env.MUXVIA_ACCOUNT_PASSWORD;

test.beforeAll(async () => {
  if (!publicURL || !accountEmail || !accountPassword) throw new Error("OPSCAT001 public URL and account credentials are required");
  await mkdir("../../../../.artifacts/opscat001", { recursive: true });
});

test("operator publishes catalog and applies typed privilege from the real UI", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 960 });
  await loginOperator(page);
  await expect(page.getByRole("heading", { name: "Control plane" })).toBeVisible();
  await expect(page.getByTestId("operator-catalog")).toBeVisible();

  const editor = page.getByTestId("catalog-editor");
  const contract = JSON.parse(await editor.inputValue()) as {
    catalogVersion: string;
    plans: Array<{ planVersion: string }>;
  };
  const nextVersion = BigInt(contract.catalogVersion) + 1n;
  contract.catalogVersion = nextVersion.toString();
  for (const plan of contract.plans) plan.planVersion = (BigInt(plan.planVersion) + 1n).toString();
  await editor.fill(JSON.stringify(contract, null, 2));
  await page.getByTestId("catalog-reason").fill("Playwright catalog release verification");
  await page.getByTestId("catalog-publish").click();
  await expect(page.getByText(`Catalog ${nextVersion.toString()}`, { exact: true })).toBeVisible();

  await page.locator('[data-testid^="operator-account-"]').first().click();
  await expect(page.getByTestId("operator-overrides")).toBeVisible();
  await page.getByTestId("override-value").fill("6");
  const until = new Date(Date.now() + 60 * 60 * 1000);
  const local = new Date(until.getTime() - until.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
  await page.getByTestId("override-until").fill(local);
  await page.getByTestId("override-reason").fill("Playwright temporary support grant");
  await page.getByTestId("override-create").click();
  await expect(page.getByTestId("operator-overrides").getByText("cloud_device_limit", { exact: true })).toBeVisible();
  await expect(page.getByTestId("operator-overrides").getByText("ACTIVE", { exact: true })).toBeVisible();
  await expect(page.getByText(/Projection 2/).first()).toBeVisible();
  await page.screenshot({ path: "../../../../.artifacts/opscat001/operator-desktop.png", fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await page.screenshot({ path: "../../../../.artifacts/opscat001/operator-mobile.png", fullPage: true });
});

async function loginOperator(page: import("@playwright/test").Page) {
  await page.goto(`${publicURL}/login`);
  await page.getByTestId("account-email").fill(accountEmail!);
  await page.getByTestId("account-password").fill(accountPassword!);
  await page.getByTestId("account-submit").click();
  await expect(page).toHaveURL(/\/account/);
  await page.goto(`${publicURL}/operator`);
  await page.getByTestId("operator-reauth").getByLabel(/Confirm account password/).fill(accountPassword!);
  await page.getByRole("button", { name: "Unlock changes" }).click();
  await expect(page.getByText("CHANGES UNLOCKED")).toBeVisible();
}
