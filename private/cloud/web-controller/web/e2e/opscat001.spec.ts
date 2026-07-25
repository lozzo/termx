import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const operatorURL = process.env.MUXVIA_OPERATOR_URL;
const operatorToken = process.env.MUXVIA_OPERATOR_TOKEN;

test.beforeAll(async () => {
  if (!operatorURL || !operatorToken) throw new Error("MUXVIA_OPERATOR_URL and MUXVIA_OPERATOR_TOKEN are required");
  await mkdir("../../../../.artifacts/opscat001", { recursive: true });
});

test("operator publishes catalog and applies typed privilege from the real UI", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${operatorURL}/operator`);
  await page.getByTestId("operator-token").fill(operatorToken!);
  await page.getByTestId("operator-submit").click();
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
