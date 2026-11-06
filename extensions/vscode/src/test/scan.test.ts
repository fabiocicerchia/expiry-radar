/**
 * The journey the extension exists for, end to end: a folder with a config,
 * the Refresh command, the real binary, and what the user is told afterwards.
 *
 * `activate.test.ts` covers the wiring and `contract.test.ts` the CLI on its
 * own; this is the two together, through the command a user actually clicks,
 * observed only through the editor the shim records.
 *
 * Offline by construction: the only host configured is a closed port on
 * loopback.
 */
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { test } from "node:test";

import { activate } from "../extension";
import { findOnPath } from "../runner";
import { commands, logged, prompts, resetShim, testConfiguration, Uri, workspace } from "./vscode-shim";

/** Walked up to, as in `contract.test.ts`: the bundle in `out/` is one level shallower than the source. */
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

/** `make build` first. Skipped rather than failed: not every checkout has one. */
const skip = binary ? false : "no expiry-radar binary — run `make build` at the repository root";

/** A closed port: refused instantly, so the TLS source fails without leaving the machine. */
const REFUSED = "127.0.0.1:1";

/** Activate in a fresh project holding `config`, run the body, and always tear down. */
async function inProject(config: string, body: (project: string) => Promise<void>): Promise<void> {
  const project = fs.mkdtempSync(path.join(os.tmpdir(), "expiry-radar-scan-"));
  fs.writeFileSync(path.join(project, "expiry-radar.json"), config);
  resetShim();
  workspace.workspaceFolders = [{ uri: Uri.file(project), name: "scan", index: 0 }];
  testConfiguration.set("expiryRadar.path", binary);
  // Manual: only the command under test may start a collection.
  testConfiguration.set("expiryRadar.scan.trigger", "manual");
  testConfiguration.set("expiryRadar.scan.onStartup", false);
  testConfiguration.set("expiryRadar.scan.timeoutSeconds", 20);
  const context = { subscriptions: [] as { dispose(): unknown }[] };
  activate(context as never);
  try {
    await body(project);
  } finally {
    for (const d of context.subscriptions.reverse()) d.dispose();
    fs.rmSync(project, { recursive: true, force: true });
  }
}

test("refreshing collects from the folder's config and says the inventory is incomplete", { skip }, async () => {
  await inProject(JSON.stringify({ endpoints: [{ host: REFUSED }] }), async (project) => {
    const collected = await commands.executeCommand("expiryRadar.scan");

    // Exit 3 is still a report: the panel has a snapshot to show.
    assert.equal(collected, true);
    assert.ok(
      logged.some((l) => l.startsWith("collect (command): ") && l.includes(path.join(project, "expiry-radar.json"))),
      logged.join("\n"),
    );
    // A lost source must not look like a clean estate — and is not an error.
    assert.deepEqual(prompts, ["warning: expiry-radar: 1 source(s) failed — this inventory is incomplete."]);
  });
});

test("refreshing with a broken config says why, and leaves the panel empty", { skip }, async () => {
  await inProject("{ not json", async () => {
    const collected = await commands.executeCommand("expiryRadar.scan");

    assert.equal(collected, false);
    assert.equal(prompts.length, 1);
    assert.match(prompts[0], /^error: expiry-radar: expiry-radar failed \(exit 2\): /);
  });
});
