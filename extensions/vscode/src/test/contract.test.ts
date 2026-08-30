/**
 * The contract with the CLI, checked against the real binary.
 *
 * Everything else in here is tested against fixtures, which proves the
 * extension is self-consistent and nothing about whether it still agrees with
 * the tool it drives. These cases run `expiry-radar` for real: the JSON shape,
 * the `expiry-radar: warning:` framing that is the only evidence a source
 * failed, and the exit codes that decide whether there is a report to read.
 *
 * Offline by construction — the one endpoint is a closed port on loopback — so
 * this is a unit test, not an integration suite that needs the internet.
 */
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { test } from 'node:test';

import { Settings } from '../config';
import { collect, findOnPath, runRadar } from '../runner';
import { Uri } from './vscode-shim';

/**
 * The checkout this extension lives in. Walked up to rather than counted in
 * `..`s: the tests are bundled into `out/`, which is one directory shallower
 * than the `src/test/` they are written in, so a fixed depth is wrong in
 * exactly one of the two places it is read.
 */
function findRepoRoot(from: string): string {
  for (let dir = from; ; ) {
    if (fs.existsSync(path.join(dir, 'go.mod')) && fs.existsSync(path.join(dir, 'cmd', 'expiry-radar'))) {
      return dir;
    }
    const up = path.dirname(dir);
    if (up === dir) return from;
    dir = up;
  }
}

const repoRoot = findRepoRoot(__dirname);
const built = path.join(repoRoot, 'bin', process.platform === 'win32' ? 'expiry-radar.exe' : 'expiry-radar');
const binary = fs.existsSync(built) ? built : findOnPath('expiry-radar');

/** `make build` first. Skipped rather than failed: not every checkout has one. */
const skip = binary ? false : 'no expiry-radar binary — run `make build` at the repository root';

const folder = { uri: Uri.file(repoRoot), name: 'expiry-radar', index: 0 } as unknown as Parameters<
  typeof collect
>[0]['folder'];

function settings(): Settings {
  return {
    // Explicit, so a stray expiry-radar.json in the checkout cannot make these
    // cases depend on somebody's estate.
    path: binary,
    configPath: '',
    endpoints: [],
    domains: [],
    extraArgs: [],
    trigger: 'manual',
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

const noToken = {
  isCancellationRequested: false,
  onCancellationRequested: () => ({ dispose() {} }),
} as unknown as Parameters<typeof collect>[2];

test('a report that lost a source is still a report, and says which one', { skip }, async () => {
  // Port 1 on loopback refuses instantly: the TLS source fails, the run exits 3
  // with partial results, and the failure is on stderr rather than swallowed.
  const { report, result } = await collect(
    { folder, endpoints: ['127.0.0.1:1'], ignoreConfig: true, reason: 'contract' },
    settings(),
    noToken,
  );
  assert.equal(result.exitCode, 3);
  assert.equal(report.items.length, 0);
  assert.equal(result.warnings.length, 1);
  assert.match(result.warnings[0], /^tls:/);
});

test('the JSON report carries every field the panel reads', { skip }, async () => {
  const { report } = await collect(
    { folder, endpoints: ['127.0.0.1:1'], ignoreConfig: true, reason: 'contract' },
    settings(),
    noToken,
  );
  assert.equal(typeof report.generatedAt, 'string');
  assert.equal(typeof report.count, 'number');
  assert.equal(typeof report.expired, 'number');
  assert.ok(Array.isArray(report.items));
});

test('a run with no sources at all is an error, not an empty inventory', { skip }, async () => {
  // Exit 2. An empty panel here would say "nothing expires", which is the one
  // thing this tool must never say when it did not look.
  await assert.rejects(
    () => collect({ folder, ignoreConfig: true, reason: 'contract' }, settings(), noToken),
    /exit 2/,
  );
});

test('the HTML report is a self-contained document', { skip }, async () => {
  const result = await runRadar(
    { folder, format: 'html', endpoints: ['127.0.0.1:1'], ignoreConfig: true, reason: 'contract' },
    settings(),
    noToken,
  );
  assert.match(result.stdout, /^<!doctype html>/i);
  // The webview injects its CSP after this exact tag, and adapts nothing else.
  assert.ok(result.stdout.includes('<meta charset="utf-8">'));
  assert.ok(result.stdout.includes('</body>'));
  // No external fetches: the report is meant to survive being mailed, and the
  // webview CSP denies everything it would need if it did not.
  assert.ok(!/<(script|link|img)[^>]+(src|href)="https?:/i.test(result.stdout));
});

test('the iCal feed is what a calendar will accept', { skip }, async () => {
  const result = await runRadar(
    { folder, format: 'ical', endpoints: ['127.0.0.1:1'], ignoreConfig: true, reason: 'contract' },
    settings(),
    noToken,
  );
  assert.match(result.stdout, /^BEGIN:VCALENDAR/);
});
