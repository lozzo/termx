import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";

type SupervisorManifest = {
  controller: { operator_url: string };
  credentials_path: string;
};

type DevelopmentCredentials = {
  operator_access_token: string;
};

test("operator UI observes two Edges and issues a real management command", async ({
  page,
}) => {
  const manifestPath = process.env.MUXVIA_CLOUD_DEV_MANIFEST;
  if (!manifestPath) throw new Error("MUXVIA_CLOUD_DEV_MANIFEST is required");
  const manifest = JSON.parse(
    await readFile(manifestPath, "utf8"),
  ) as SupervisorManifest;
  const credentials = JSON.parse(
    await readFile(manifest.credentials_path, "utf8"),
  ) as DevelopmentCredentials;

  await page.goto(`${manifest.controller.operator_url}/operator`);
  await page.getByTestId("operator-token").fill(credentials.operator_access_token);
  await page.getByTestId("operator-submit").click();
  await expect(page.getByText("Hub and Relay fleet")).toBeVisible();
  await expect(page.locator("article").filter({ hasText: "hub-edge-a" })).toBeVisible();
  await expect(page.locator("article").filter({ hasText: "hub-edge-b" })).toBeVisible();

  const account = page.locator('[data-testid^="operator-account-"]').first();
  await account.click();
  await expect(page.getByRole("heading", { name: "Devices" })).toBeVisible();
  await expect(
    page.getByTestId("migrate-daemon-edge-a-hub-edge-a"),
  ).toHaveCount(0);
  await expect(
    page.locator('[data-testid^="migrate-client-dev-local-"]'),
  ).toHaveCount(0);
  const migrationResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/operator/commands") &&
      response.request().method() === "POST",
  );
  await page.getByTestId("migrate-daemon-edge-a-hub-edge-b").click();
  await expect((await migrationResponse).status()).toBe(202);
  const revoke = page.getByTestId("revoke-client-dev-local");
  await expect(revoke).toBeEnabled();
  await revoke.click();
  await expect(revoke).toBeDisabled();
});
