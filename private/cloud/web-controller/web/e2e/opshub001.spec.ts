import { expect, test, type Page, type Route } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const baseURL = process.env.MUXVIA_OPSHUB_E2E_BASE_URL ?? "http://127.0.0.1:5173";

test.beforeAll(async () => {
  await mkdir("../../../../.artifacts/opshub001", { recursive: true });
});

test("operator creates, edits, approves, drains and archives a Hub", async ({ page }) => {
  await installOperatorAPI(page);
  await page.setViewportSize({ width: 1366, height: 950 });
  await page.goto(`${baseURL}/operator`);

  await page.getByText("Add Hub deployment").click();
  const createPanel = page.getByTestId("hub-create-panel");
  await createPanel.getByLabel("Hub ID").fill("hub-dynamic");
  await createPanel.getByLabel("Edge deployment ID").fill("edge-dynamic");
  await createPanel.getByLabel("Relay ID").fill("relay-dynamic");
  await createPanel.getByLabel("Region").fill("cn-east-2");
  await createPanel.getByLabel("Public label").fill("China Dynamic");
  await createPanel.getByLabel("Public Hub URL").fill("https://cn2.edge.muxvia.test");
  await createPanel.getByLabel("Health URL").fill("https://cn2.edge.muxvia.test/healthz");
  await createPanel.getByLabel("Capacity").fill("500");
  await createPanel.getByLabel("Hub control public key").fill(base64Key(1));
  await createPanel.getByLabel("Relay control public key").fill(base64Key(2));
  await createPanel.getByLabel("Change reason").fill("Add a lower latency region");
  await createPanel.getByRole("button", { name: "Create pending deployment" }).click();

  let card = page.getByTestId("hub-hub-dynamic");
  await expect(card).toContainText("Pending approval");
  await expect(card).toContainText("Hub: hub-fingerprint");
  await card.getByRole("button", { name: "Edit" }).click();
  await card.getByLabel("Public label").fill("China East Dynamic");
  await card.getByLabel("Change reason").fill("Clarify operator label");
  await card.getByRole("button", { name: "Save directory" }).click();
  card = page.getByTestId("hub-hub-dynamic");
  await expect(card).toContainText("China East Dynamic");

  await card.getByRole("button", { name: "Approve identity" }).click();
  await expect(card).toContainText("Active");
  await card.getByRole("button", { name: "Start drain" }).click();
  await expect(card).toContainText("Draining");
  page.once("dialog", (dialog) => dialog.accept());
  await card.getByRole("button", { name: "Disable" }).click();
  await expect(card).toContainText("Archived");
  await page.screenshot({ path: "../../../../.artifacts/opshub001/hub-lifecycle-desktop.png", fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(1);
  await page.screenshot({ path: "../../../../.artifacts/opshub001/hub-lifecycle-mobile.png", fullPage: true });
});

async function installOperatorAPI(page: Page) {
  let deployment: Record<string, unknown> | undefined;
  await page.route("**/api/v1/operator/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = route.request().postDataJSON?.() as Record<string, unknown> | undefined;
    if (path === "/api/v1/operator/fleet/create") {
      deployment = hubDeployment(body ?? {});
      return json(route, { deployment });
    }
    if (path === "/api/v1/operator/fleet/update" && deployment) {
      const metadata = deployment.metadata as Record<string, unknown>;
      metadata.region = body?.region;
      metadata.publicLabel = body?.public_label;
      deployment.publicHubUrl = body?.public_hub_url;
      deployment.healthUrl = body?.health_url;
      deployment.maxAssignments = body?.max_assignments;
      deployment.directoryRevision = "2";
      return json(route, { deployment });
    }
    if (path === "/api/v1/operator/fleet/approve" && deployment) {
      deployment.identityApproved = true;
      deployment.enabled = true;
      deployment.directoryRevision = "3";
      return json(route, { deployment });
    }
    if (path === "/api/v1/operator/fleet/drain" && deployment) {
      deployment.draining = body?.draining;
      deployment.directoryRevision = "4";
      return json(route, { deployment });
    }
    if (path === "/api/v1/operator/fleet/disable" && deployment) {
      deployment.enabled = false;
      deployment.archived = true;
      deployment.directoryRevision = "5";
      return json(route, { deployment });
    }
    if (path === "/api/v1/operator/fleet/list") {
      return json(route, { hubs: deployment ? [{ deployment, activeAssignments: "0", freshness: "FRESHNESS_STALE" }] : [], page: {} });
    }
    return json(route, {});
  });
}

function hubDeployment(body: Record<string, unknown>) {
  return {
    metadata: {
      hubId: body.hub_id,
      edgeDeploymentId: body.edge_deployment_id,
      relayId: body.relay_id,
      region: body.region,
      publicLabel: body.public_label,
      hubControlIdentityFingerprint: "hub-fingerprint",
      relayControlIdentityFingerprint: "relay-fingerprint",
    },
    publicHubUrl: body.public_hub_url,
    healthUrl: body.health_url,
    maxAssignments: body.max_assignments,
    directoryRevision: "1",
    updatedAtUnixMillis: "1784973600000",
  };
}

function base64Key(value: number) {
  return btoa(String.fromCharCode(...new Uint8Array(32).fill(value)));
}

async function json(route: Route, body: unknown) {
  await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}
