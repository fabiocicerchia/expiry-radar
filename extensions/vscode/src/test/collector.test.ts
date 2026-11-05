/**
 * `Collector.execute` against its real collaborators: the `expiry-radar`
 * binary, a config file on disk, and the store, publisher, status bar and panel
 * it wires together. Each of those is tested alone; this proves a run joins
 * them — the item the CLI reports lands on the config line that declared it.
 *
 * Offline by construction: only `manual` entries, which need no network.
 */
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { test } from "node:test";

import { Collector } from "../collector";
import { DiagnosticPublisher } from "../diagnostics";
import { InventoryView } from "../inventoryView";
import { findOnPath } from "../runner";
import { StatusBar } from "../status";
import { ResultStore } from "../store";
import {
  CancellationTokenSource,
  Diagnostic,
  DiagnosticSeverity,
  resetShim,
  testConfiguration,
  Uri,
  workspace,
} from "./vscode-shim";

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

/** What the publisher wrote, by file. The collection is the editor's, not ours. */
function published(publisher: DiagnosticPublisher): Map<string, Diagnostic[]> {
  return (publisher as unknown as { collection: { _entries: Map<string, Diagnostic[]> } }).collection._entries;
}

test("a collection places each recorded item on the config line that declared it", { skip }, async () => {
  const project = fs.mkdtempSync(path.join(os.tmpdir(), "expiry-radar-collector-"));
  const configPath = path.join(project, "expiry-radar.json");
  fs.writeFileSync(
    configPath,
    JSON.stringify(
      {
        manual: [
          { name: "lapsed.example", kind: "domain", expires: "2020-01-01" },
          { name: "distant.example", kind: "domain", expires: "2999-01-01" },
        ],
      },
      null,
      2,
    ),
  );
  resetShim();
  const folder = { uri: Uri.file(project), name: "collector", index: 0 };
  workspace.workspaceFolders = [folder];
  testConfiguration.set("expiryRadar.path", binary);
  testConfiguration.set("expiryRadar.scan.timeoutSeconds", 20);

  const store = new ResultStore();
  const publisher = new DiagnosticPublisher();
  const collector = new Collector(store, publisher, new StatusBar(), new InventoryView(store));
  try {
    await collector.execute(
      { folder: folder as never, reason: "test", manual: true },
      new CancellationTokenSource().token as never,
    );

    const snapshot = store.get(folder as never);
    assert.ok(snapshot, "the run left no snapshot");
    assert.equal(snapshot.configPath, configPath);
    assert.deepEqual(snapshot.items.map((i) => i.name).sort(), ["distant.example", "lapsed.example"]);

    // Only the expired one is squiggled; the distant one is the panel's business.
    const diagnostics = published(publisher).get(configPath) ?? [];
    assert.equal(diagnostics.length, 1);
    assert.equal(diagnostics[0].severity, DiagnosticSeverity.Error);
    assert.match(diagnostics[0].message, /^lapsed\.example expired \d+ day\(s\) ago/);
    const line = fs
      .readFileSync(configPath, "utf8")
      .split("\n")
      .findIndex((l) => l.includes('"lapsed.example"'));
    assert.equal(diagnostics[0].range.start.line, line);
  } finally {
    fs.rmSync(project, { recursive: true, force: true });
  }
});
