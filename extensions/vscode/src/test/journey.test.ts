/**
 * The user journeys, end to end: a command run through `activate()`, the real
 * `expiry-radar` binary underneath, and what the user is told at the end.
 *
 * `activate.test.ts` stops before the binary and `contract.test.ts` starts at
 * the runner; nothing else proves the two meet. Offline by construction —
 * every endpoint is a closed port on loopback.
 */
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { test } from "node:test";

import { activate } from "../extension";
import { findOnPath } from "../runner";
import { commands, prompts, resetShim, statusBarItems, testConfiguration, Uri, workspace } from "./vscode-shim";

/** Walked up to, as in `contract.test.ts`: `out/` and `src/test/` sit at different depths. */
function findRepoRoot(from: string): string {
  for (let dir = from; ; ) {
    if (fs.existsSync(path.join(dir, "go.mod")) && fs.existsSync(path.join(dir, "cmd", "expiry-radar"))) {
      return dir;
    }
    const up = path.dirname(dir);
    if (up === dir) return from;
    dir = up;
  }
}

const built = path.join(
  findRepoRoot(__dirname),
  "bin",
  process.platform === "win32" ? "expiry-radar.exe" : "expiry-radar",
);
const binary = fs.existsSync(built) ? built : findOnPath("expiry-radar");
const skip = binary ? false : "no expiry-radar binary — run `make build` at the repository root";

const REFUSED = "127.0.0.1:1";

/** Activate in a fresh project holding `config`, run the body, and always tear down. */
async function inProject(config: unknown, body: () => Promise<void>): Promise<void> {
  const project = fs.mkdtempSync(path.join(os.tmpdir(), "expiry-radar-journey-"));
  fs.writeFileSync(path.join(project, "expiry-radar.json"), JSON.stringify(config, null, 2));
  resetShim();
  workspace.workspaceFolders = [{ uri: Uri.file(project), name: "journey", index: 0 }];
  testConfiguration.set("expiryRadar.path", binary);
  testConfiguration.set("expiryRadar.scan.trigger", "manual");
  testConfiguration.set("expiryRadar.scan.onStartup", false);
  testConfiguration.set("expiryRadar.scan.timeoutSeconds", 20);
  const context = { subscriptions: [] as { dispose(): unknown }[] };
  activate(context as never);
  try {
    await body();
  } finally {
    for (const d of context.subscriptions.reverse()) d.dispose();
    fs.rmSync(project, { recursive: true, force: true });
  }
}

test("refreshing an inventory that lost a source says it is incomplete", { skip }, async () => {
  await inProject({ endpoints: [{ host: REFUSED }] }, async () => {
    await commands.executeCommand("expiryRadar.scan");
    // An empty panel with no warning would read as a clean estate.
    assert.deepEqual(prompts, ["warning: expiry-radar: 1 source(s) failed — this inventory is incomplete."]);
  });
});

test("refreshing an inventory with something expired turns the status bar red", { skip }, async () => {
  // A recorded item needs no network: the whole run stays on this machine.
  await inProject({ manual: [{ name: "lapsed.example", kind: "domain", expires: "2020-01-01" }] }, async () => {
    await commands.executeCommand("expiryRadar.scan");
    assert.deepEqual(prompts, []);
    assert.equal(statusBarItems.length, 1);
    assert.match(statusBarItems[0].text, /^\$\(error\) expiry-radar /);
  });
});
