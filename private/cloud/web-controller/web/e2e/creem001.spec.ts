import { expect, test, type Page, type Route } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const baseURL = process.env.MUXVIA_CREEM_E2E_BASE_URL ?? "http://127.0.0.1:5173";

test.beforeAll(async () => {
  await mkdir("../../../../.artifacts/creem001", { recursive: true });
});

test("operator sees Creem truth and can request immediate reconciliation", async ({ page }) => {
  let reconciliations = 0;
  await installOperatorAPI(page, () => { reconciliations++; });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`${baseURL}/operator`);

  const attempt = page.getByTestId("payment-attempt-attempt_creem_e2e");
  await expect(attempt).toContainText("creem · PENDING");
  await expect(attempt).toContainText("ch_creem_e2e");
  await expect(attempt).toContainText("tran_creem_e2e");
  await expect(attempt).toContainText("sub_creem_e2e");
  await expect(attempt).toContainText("processing");
  await expect(page.getByRole("button", { name: "Record payment" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Refund" })).toHaveCount(0);

  await attempt.getByLabel("Reconciliation reason for attempt_creem_e2e").fill("Webhook delivery is delayed");
  await attempt.getByRole("button", { name: "Reconcile now" }).click();
  await expect.poll(() => reconciliations).toBe(1);
  await expect(page.getByTestId("payment-attempt-attempt_creem_e2e")).toContainText("operator_reconciled");
  await page.screenshot({ path: "../../../../.artifacts/creem001/operator-reconciliation.png", fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await page.screenshot({ path: "../../../../.artifacts/creem001/operator-reconciliation-mobile.png", fullPage: true });
});

async function installOperatorAPI(page: Page, reconciled: () => void) {
  let providerStatus = "processing";
  await page.route("**/api/v1/operator/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/operator/orders/reconcile-creem") {
      providerStatus = "operator_reconciled";
      reconciled();
      return json(route, { payment_attempt: paymentAttempt(providerStatus) });
    }
    if (path === "/api/v1/operator/orders/list") {
      return json(route, {
        orders: [{
          order: {
            order_id: "order_creem_e2e",
            account_id: "account_creem_e2e",
            plan_id: "pro",
            plan_version: "3",
            status: "ORDER_STATUS_PENDING",
            price: { currency: "USD" },
            total_minor: "1000",
          },
          payment_attempts: [paymentAttempt(providerStatus)],
          payment_events: [],
        }],
        page: {},
      });
    }
    return json(route, {});
  });
}

function paymentAttempt(providerStatus: string) {
  return {
    payment_attempt_id: "attempt_creem_e2e",
    order_id: "order_creem_e2e",
    account_id: "account_creem_e2e",
    provider: "creem",
    status: "PAYMENT_ATTEMPT_STATUS_PENDING",
    provider_reference: "ch_creem_e2e",
    provider_transaction_reference: "tran_creem_e2e",
    provider_subscription_reference: "sub_creem_e2e",
    reconcile_after_unix_millis: "1784980800000",
    reconcile_attempts: 4,
    last_provider_status: providerStatus,
    revision: "7",
  };
}

async function json(route: Route, body: unknown) {
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}
