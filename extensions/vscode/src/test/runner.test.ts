import assert from 'node:assert/strict';
import { test } from 'node:test';

import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';

import { Settings } from '../config';
import { buildArgs, hasSources, RunRequest } from '../runner';
import { Uri } from './vscode-shim';

const folder = { uri: Uri.file('/repo'), name: 'repo', index: 0 } as unknown as RunRequest['folder'];

function settings(over: Partial<Settings> = {}): Settings {
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
    timeoutSeconds: 120,
    diagnosticsEnabled: true,
    warnWithinDays: 14,
    infoWithinDays: 30,
    withinDays: 0,
    minPriority: 0,
    statusWarnWithinDays: 14,
    ...over,
  };
}

function request(over: Partial<RunRequest> = {}): RunRequest {
  return { folder, format: 'json', reason: 'test', ...over };
}

test('the format and the collection budget are always passed', () => {
  assert.deepEqual(buildArgs(request(), settings(), ''), ['-format', 'json', '-timeout', '120s']);
});

test('a resolved config file is passed as -config', () => {
  const args = buildArgs(request(), settings(), '/repo/expiry-radar.json');
  assert.deepEqual(args.slice(0, 4), ['-format', 'json', '-config', '/repo/expiry-radar.json']);
});

test('settings endpoints and domains add to the config, comma-joined', () => {
  const args = buildArgs(
    request(),
    settings({ endpoints: ['a.example.com', 'b.example.com:8443'], domains: ['example.com'] }),
    '/repo/expiry-radar.json',
  );
  assert.equal(args[args.indexOf('-endpoints') + 1], 'a.example.com,b.example.com:8443');
  assert.equal(args[args.indexOf('-domains') + 1], 'example.com');
});

test('a one-off probe drops the config, the settings and the extra args', () => {
  const args = buildArgs(
    request({ endpoints: ['probe.example.com'], domains: ['example.com'], ignoreConfig: true }),
    settings({ endpoints: ['configured.example.com'], extraArgs: ['-fail-within', '7'] }),
    '',
  );
  assert.equal(args[args.indexOf('-endpoints') + 1], 'probe.example.com');
  assert.ok(!args.includes('-config'));
  assert.ok(!args.includes('-fail-within'));
});

test('the view filters are pushed down to the CLI, and only when set', () => {
  assert.ok(!buildArgs(request(), settings(), '').includes('-within'));
  const args = buildArgs(request(), settings({ withinDays: 45, minPriority: 0.3 }), '');
  assert.equal(args[args.indexOf('-within') + 1], '45');
  assert.equal(args[args.indexOf('-min-priority') + 1], '0.3');
});

test('extra args go last, so a user can override what we chose', () => {
  const args = buildArgs(request(), settings({ extraArgs: ['-timeout', '5s'] }), '');
  assert.deepEqual(args.slice(-2), ['-timeout', '5s']);
});

test('the format is whatever the caller asked to render', () => {
  assert.equal(buildArgs(request({ format: 'ical' }), settings(), '')[1], 'ical');
  assert.equal(buildArgs(request({ format: 'html' }), settings(), '')[1], 'html');
});

test('a folder with no config and no settings has nothing to collect', () => {
  // Every source is opt-in, so this is the common case in a repository that has
  // nothing to do with this tool — and the reason nothing is collected on
  // startup there.
  const empty = fs.mkdtempSync(path.join(os.tmpdir(), 'expiry-radar-'));
  const bare = { uri: Uri.file(empty), name: 'empty', index: 0 } as unknown as RunRequest['folder'];
  try {
    assert.equal(hasSources(bare, settings()), false);
    assert.equal(hasSources(bare, settings({ endpoints: ['a.example.com'] })), true);
    assert.equal(hasSources(bare, settings({ domains: ['example.com'] })), true);

    fs.writeFileSync(path.join(empty, 'expiry-radar.json'), '{}');
    assert.equal(hasSources(bare, settings()), true);
  } finally {
    fs.rmSync(empty, { recursive: true, force: true });
  }
});
