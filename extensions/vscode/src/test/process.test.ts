/**
 * `runRadar` against a real child process: what the operating system does
 * when a collection is cancelled, and which exit codes still carry a report.
 *
 * `contract.test.ts` needs the real binary and cannot make it hang or exit 1
 * on demand; a stand-in shell script can, so these run on every checkout.
 */
import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { test } from "node:test";

import { Settings } from "../config";
import { resetBinaryCache, runRadar } from "../runner";
import { CancellationError, CancellationTokenSource, Uri } from "./vscode-shim";

/** Process groups and `#!/bin/sh` are POSIX; Windows takes the taskkill path. */
const skip = process.platform === "win32" ? "stand-in binaries are shell scripts" : false;

function settings(binary: string): Settings {
  return {
    path: binary,
    configPath: "",
    endpoints: [],
    domains: [],
    extraArgs: [],
    trigger: "manual",
    scanOnStartup: false,
    intervalMinutes: 60,
    debounceMs: 1500,
    timeoutSeconds: 20,
    diagnosticsEnabled: true,
    warnWithinDays: 14,
    infoWithinDays: 30,
    withinDays: 0,
    minPriority: 0,
    statusWarnWithinDays: 14,
  };
}

/** A fresh folder holding a stand-in binary with `script` as its body; always torn down. */
async function withStandIn(
  script: string,
  body: (folder: never, s: Settings, dir: string) => Promise<void>,
): Promise<void> {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "expiry-radar-process-"));
  const binary = path.join(dir, "fake-radar");
  fs.writeFileSync(binary, `#!/bin/sh\n${script}\n`, { mode: 0o755 });
  resetBinaryCache();
  try {
    await body({ uri: Uri.file(dir), name: "process", index: 0 } as never, settings(binary), dir);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

async function until(cond: () => boolean, what: string, ms = 5000): Promise<void> {
  for (const deadline = Date.now() + ms; !cond(); ) {
    if (Date.now() > deadline) assert.fail(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 25));
  }
}

test("cancelling a collection kills what the CLI started, not just the CLI", { skip }, async () => {
  // The grandchild stands in for an in-flight TLS dial or cloud SDK call.
  await withStandIn("sleep 60 &\necho $! > grandchild.pid\nwait", async (folder, s, dir) => {
    const source = new CancellationTokenSource();
    const run = runRadar({ folder, format: "json", reason: "test" }, s, source.token as never);

    const pidFile = path.join(dir, "grandchild.pid");
    const pid = () => (fs.existsSync(pidFile) ? Number(fs.readFileSync(pidFile, "utf8")) : 0);
    await until(() => pid() > 0, "the grandchild");
    const grandchild = pid();
    assert.ok(alive(grandchild));

    source.cancel();
    await assert.rejects(run, CancellationError);
    await until(() => !alive(grandchild), "the grandchild to die");
  });
});

test("exit 1 with a report on stdout is a report, and its warnings are kept", { skip }, async () => {
  // What a user's `-fail-within` in extraArgs produces: a breached threshold,
  // and a perfectly good inventory alongside it.
  const script = `echo '{"items":[]}'\necho 'expiry-radar: warning: vault: permission denied' >&2\nexit 1`;
  await withStandIn(script, async (folder, s) => {
    const token = new CancellationTokenSource().token as never;
    const result = await runRadar({ folder, format: "json", reason: "test" }, s, token);

    assert.equal(result.exitCode, 1);
    assert.deepEqual(JSON.parse(result.stdout), { items: [] });
    assert.deepEqual(result.warnings, ["vault: permission denied"]);
  });
});

test("exit 2 is a failure that carries the CLI's own message", { skip }, async () => {
  await withStandIn(`echo '{"items":[]}'\necho 'expiry-radar: bad -within' >&2\nexit 2`, async (folder, s) => {
    const token = new CancellationTokenSource().token as never;
    await assert.rejects(
      runRadar({ folder, format: "json", reason: "test" }, s, token),
      /^Error: expiry-radar failed \(exit 2\): expiry-radar: bad -within$/,
    );
  });
});
