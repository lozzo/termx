import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawn } from "node:child_process";

const webRoot = process.cwd();
const repoRoot = resolve(webRoot, "../../../..");
const artifactDir = await mkdtemp(join(tmpdir(), "termx-hub007-"));
const manifestPath = join(artifactDir, "runtime.json");
const supervisor = spawn(
  "go",
  [
    "run",
    "./private/cloud/devcloud/cmd/termx-cloud-dev",
    "--manifest",
    manifestPath,
    "--repo-root",
    repoRoot,
  ],
  { cwd: repoRoot, stdio: "inherit" },
);

async function waitForManifest() {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    if (supervisor.exitCode !== null)
      throw new Error(`cloud supervisor exited with ${supervisor.exitCode}`);
    try {
      const value = JSON.parse(await readFile(manifestPath, "utf8"));
      if (value.controller?.operator_url && value.edges?.length === 2) return;
    } catch {
      // Supervisor writes the manifest atomically after all child health checks pass.
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 200));
  }
  throw new Error("timed out waiting for cloud supervisor manifest");
}

try {
  await waitForManifest();
  const playwright = spawn(
    process.platform === "win32" ? "npx.cmd" : "npx",
    ["playwright", "test", "e2e/hub007.spec.ts"],
    {
      cwd: webRoot,
      stdio: "inherit",
      env: { ...process.env, TERMX_CLOUD_DEV_MANIFEST: manifestPath },
    },
  );
  const exitCode = await new Promise((resolveExit) =>
    playwright.on("exit", resolveExit),
  );
  if (exitCode !== 0) process.exitCode = Number(exitCode ?? 1);
} finally {
  supervisor.kill("SIGTERM");
  await new Promise((resolveExit) => supervisor.on("exit", resolveExit));
}
