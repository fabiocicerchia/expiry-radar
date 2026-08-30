import assert from 'node:assert/strict';
import { test } from 'node:test';

import { Settings } from '../config';
import { DiagnosticPublisher, message } from '../diagnostics';
import { Item } from '../types';
import { Diagnostic, DiagnosticSeverity, Uri } from './vscode-shim';

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

function item(over: Partial<Item> = {}): Item {
  return {
    id: 'id',
    display: 'shop.example.com',
    severity: 'urgent',
    priority: 0.7,
    blastRadius: 0.8,
    daysLeft: 5,
    expired: false,
    kind: 'tls_cert',
    name: 'shop.example.com',
    source: 'tls:endpoint',
    expires: '2026-09-04T12:00:00Z',
    why: 'public endpoint, 2400 req/s',
    origin: { file: '/repo/expiry-radar.json', line: 3, column: 15 },
    ...over,
  };
}

/** What the publisher wrote, by file. The collection is the editor's, not ours. */
function published(publisher: DiagnosticPublisher): Map<string, Diagnostic[]> {
  return (publisher as unknown as { collection: { _entries: Map<string, Diagnostic[]> } }).collection
    ._entries;
}

test('the message says what expires, when, and why it is ranked where it is', () => {
  const text = message(item());
  assert.ok(text.startsWith('shop.example.com expires in 5d.'));
  assert.ok(text.includes('public endpoint, 2400 req/s'));
  assert.ok(text.includes('blast radius: 0.80'));
});

test('an expired item says how long it has been broken', () => {
  assert.ok(message(item({ daysLeft: -3.2 })).startsWith('shop.example.com expired 3 day(s) ago.'));
});

test('severity follows the deadline, on the configured windows', () => {
  const publisher = new DiagnosticPublisher();
  publisher.publish([
    {
      items: [
        item({ daysLeft: -1 }),
        item({ daysLeft: 5 }),
        item({ daysLeft: 20 }),
        item({ daysLeft: 90 }),
      ],
      settings: settings(),
    },
  ]);
  assert.deepEqual(
    published(publisher)
      .get('/repo/expiry-radar.json')!
      .map((d) => d.severity),
    [DiagnosticSeverity.Error, DiagnosticSeverity.Warning, DiagnosticSeverity.Information],
  );
  publisher.dispose();
});

test('an item nothing in the repository declared gets no squiggle', () => {
  const publisher = new DiagnosticPublisher();
  publisher.publish([
    { items: [item({ origin: undefined, source: 'k8s:ingress' })], settings: settings() },
  ]);
  assert.equal(published(publisher).size, 0);
  publisher.dispose();
});

test('diagnostics can be turned off per folder without emptying the panel', () => {
  const publisher = new DiagnosticPublisher();
  publisher.publish([{ items: [item()], settings: settings({ diagnosticsEnabled: false }) }]);
  assert.equal(published(publisher).size, 0);
  publisher.dispose();
});

test('a republish replaces the previous one wholesale', () => {
  const publisher = new DiagnosticPublisher();
  publisher.publish([{ items: [item()], settings: settings() }]);
  publisher.publish([{ items: [], settings: settings() }]);
  assert.equal(published(publisher).size, 0);
  publisher.dispose();
});

test('the range starts at the declared column and runs to end of line', () => {
  const publisher = new DiagnosticPublisher();
  publisher.publish([{ items: [item()], settings: settings() }]);
  const [diagnostic] = published(publisher).get('/repo/expiry-radar.json')!;
  assert.equal(diagnostic.range.start.line, 2); // 1-based in the config, 0-based here.
  assert.equal(diagnostic.range.start.character, 14);
  assert.equal(diagnostic.range.end.character, Number.MAX_SAFE_INTEGER);
  assert.equal(diagnostic.source, 'expiry-radar');
  assert.equal(diagnostic.code, 'tls_cert');
  publisher.dispose();
});

test('two folders publish into one collection without displacing each other', () => {
  const publisher = new DiagnosticPublisher();
  publisher.publish([
    { items: [item()], settings: settings() },
    {
      items: [item({ origin: { file: '/other/expiry-radar.json', line: 2, column: 5 } })],
      settings: settings(),
    },
  ]);
  assert.deepEqual([...published(publisher).keys()].sort(), [
    '/other/expiry-radar.json',
    '/repo/expiry-radar.json',
  ]);
  publisher.dispose();
});

test('Uri.file is what the collection is keyed by', () => {
  assert.equal(Uri.file('/repo/expiry-radar.json').fsPath, '/repo/expiry-radar.json');
});
