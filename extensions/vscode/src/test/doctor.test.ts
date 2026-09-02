/**
 * What the doctor says.
 *
 * `runDoctor` returns nothing; its whole output is the report it writes into
 * the log and the one-line summary it shows. Both are asserted here, verbatim,
 * because they are the answer to "why did that come back empty" and a wrong
 * line is a wrong diagnosis.
 *
 * The binary is a two-line script written into the fixture, so "it runs" and
 * "it is not expiry-radar" are both reachable without a build and without
 * anything leaving the machine.
 */
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { test } from 'node:test';

import { Settings } from '../config';
import { runDoctor } from '../doctor';
import { logged, prompts, resetShim, Uri } from './vscode-shim';

function settings(overrides: Partial<Settings> = {}): Settings {
  return {
    path: '',
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
    ...overrides,
  };
}

/** A fixture folder with a stand-in binary that prints `usage`, and maybe a config. */
function fixture(usage: string, config?: unknown): { folder: never; binary: string; dir: string } {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'expiry-radar-doctor-'));
  const binary = path.join(dir, 'fake-radar');
  fs.writeFileSync(binary, `#!/bin/sh\ncat <<'EOF'\n${usage}\nEOF\n`, { mode: 0o755 });
  if (config !== undefined) {
    fs.writeFileSync(
      path.join(dir, 'expiry-radar.json'),
      typeof config === 'string' ? config : JSON.stringify(config),
    );
  }
  return { folder: { uri: Uri.file(dir), name: 'doctor', index: 0 } as never, binary, dir };
}

/**
 * The doctor's report, from its header on: the runner logs "using <binary>"
 * into the same channel on the way, and that line is not part of the report.
 */
function report(): { lines: string[]; summary: string | undefined } {
  const header = logged.findIndex((l) => l.startsWith('expiry-radar doctor —'));
  return {
    lines: logged.slice(header).filter((l) => l !== ''),
    summary: prompts.find((p) => p.startsWith('information: '))?.slice('information: '.length),
  };
}

test('a binary that runs and a config with both free sources reads clean', async () => {
  resetShim();
  const { folder, binary, dir } = fixture('expiry-radar — usage', {
    endpoints: [{ host: 'a' }, { host: 'b' }],
    domains: ['example.com'],
  });
  try {
    await runDoctor(folder, settings({ path: binary }));
    assert.deepEqual(report(), {
      lines: [
        'expiry-radar doctor — doctor',
        `✓ runs: ${binary}`,
        '✓ config: expiry-radar.json',
        '✓ 2 endpoint(s) to probe over TLS',
        '✓ 1 domain(s) to check via RDAP',
      ],
      summary: 'expiry-radar: everything the doctor checks looks fine.',
    });
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('something on disk that is not expiry-radar is named, not trusted', async () => {
  resetShim();
  const { folder, binary, dir } = fixture('this is git, actually', { domains: ['example.com'] });
  try {
    await runDoctor(folder, settings({ path: binary }));
    const { lines, summary } = report();
    assert.deepEqual(lines.slice(1, 3), [
      `! ${binary} ran, but does not look like expiry-radar`,
      '    this is git, actually',
    ]);
    assert.equal(
      summary,
      'expiry-radar: the doctor found something that will limit results — see the log.',
    );
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('a config that enables nothing at all is an error, and the summary says so', async () => {
  resetShim();
  const { folder, binary, dir } = fixture('expiry-radar — usage', { endpoints: [], domains: [] });
  try {
    await runDoctor(folder, settings({ path: binary }));
    const { lines, summary } = report();
    assert.deepEqual(lines.slice(-2), [
      '✗ the config file enables no sources at all',
      '    Every source is opt-in; nothing runs implicitly.',
    ]);
    assert.equal(
      summary,
      'expiry-radar: the doctor found something that stops it running — see the log.',
    );
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('a config that is not JSON is reported as unusable rather than as empty', async () => {
  resetShim();
  const { folder, binary, dir } = fixture('expiry-radar — usage', '{ not json');
  try {
    await runDoctor(folder, settings({ path: binary }));
    const { lines } = report();
    assert.equal(lines.at(-2), '✗ the config file is not valid JSON');
    assert.match(lines.at(-1) ?? '', /^ {4}SyntaxError/);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('an enabled source with no credentials in the environment is a warning each', async () => {
  resetShim();
  const { folder, binary, dir } = fixture('expiry-radar — usage', {
    k8s: { enabled: true },
    vault: { enabled: true },
    aws: { enabled: true, region: 'eu-west-1' },
  });
  const saved = { ...process.env };
  for (const key of [
    'KUBERNETES_SERVICE_HOST',
    'VAULT_ADDR',
    'VAULT_TOKEN',
    'AWS_ACCESS_KEY_ID',
    'AWS_PROFILE',
    'AWS_ROLE_ARN',
    'AWS_WEB_IDENTITY_TOKEN_FILE',
  ]) {
    delete process.env[key];
  }
  try {
    await runDoctor(folder, settings({ path: binary }));
    const { lines } = report();
    assert.deepEqual(lines.slice(3), [
      '! kubernetes is enabled with no server, and this is not a cluster pod',
      '    Run `kubectl proxy` and set "server": "http://127.0.0.1:8001", or run in-cluster.',
      '! vault is enabled with no addr and no $VAULT_ADDR',
      '! aws is enabled but the editor has no AWS credentials in its environment',
      '    The credential chain is read from the process the editor was launched from.',
    ]);
  } finally {
    process.env = saved;
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('no config and nothing in the settings is the warning that explains an empty panel', async () => {
  resetShim();
  const { folder, binary, dir } = fixture('expiry-radar — usage');
  try {
    await runDoctor(folder, settings({ path: binary }));
    const { lines } = report();
    assert.deepEqual(lines.slice(2), [
      '! no config file at expiry-radar.json, and no endpoints or domains in settings',
      '    Nothing is enabled implicitly: without one of these there are no sources to run.',
      '    Copy expiry-radar.example.json to expiry-radar.json to start.',
    ]);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
