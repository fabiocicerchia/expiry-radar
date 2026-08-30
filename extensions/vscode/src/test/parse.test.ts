import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  compareItems,
  describe,
  displayName,
  humanDays,
  kindLabel,
  parseWarnings,
  severity,
  toItems,
} from '../parse';
import { Report, ReportItem } from '../types';

function raw(over: Partial<ReportItem> = {}): ReportItem {
  return {
    priority: 0.5,
    blastRadius: 0.5,
    daysLeft: 40,
    expired: false,
    kind: 'tls_cert',
    name: 'shop.example.com',
    source: 'tls:endpoint',
    expires: '2026-10-09T12:00:00Z',
    why: 'public endpoint',
    ...over,
  };
}

test('severity follows the deadline, on the report thresholds', () => {
  assert.equal(severity(-1), 'expired');
  assert.equal(severity(0), 'urgent');
  assert.equal(severity(14), 'urgent');
  assert.equal(severity(14.5), 'soon');
  assert.equal(severity(30), 'soon');
  assert.equal(severity(31), 'ok');
});

test('severity thresholds are configurable, and 0 still means urgent-only-if-due', () => {
  assert.equal(severity(5, 3, 10), 'soon');
  assert.equal(severity(3, 3, 10), 'urgent');
  assert.equal(severity(11, 3, 10), 'ok');
});

test('humanDays never says a cheerful zero for something already broken', () => {
  assert.equal(humanDays(-3.5), 'expired 3d ago');
  assert.equal(humanDays(0.4), 'today');
  assert.equal(humanDays(1), '1d');
  assert.equal(humanDays(41.9), '41d');
});

test('displayName prefixes the namespace exactly once', () => {
  assert.equal(displayName(raw({ name: 'web-tls', namespace: 'prod' })), 'prod/web-tls');
  assert.equal(displayName(raw({ name: 'prod/web-tls', namespace: 'prod' })), 'prod/web-tls');
  assert.equal(displayName(raw()), 'shop.example.com');
});

test('kindLabel stays readable for a kind this build has never heard of', () => {
  assert.equal(kindLabel('intermediate_ca'), 'Intermediate CA');
  assert.equal(kindLabel('ssh_host_key'), 'ssh host key');
});

test('toItems assigns a stable id, and disambiguates a genuine duplicate', () => {
  const report: Report = {
    generatedAt: '2026-08-30T00:00:00Z',
    count: 2,
    expired: 0,
    items: [raw(), raw({ source: 'vault' }), raw()],
  };
  const items = toItems(report);
  assert.equal(items[0].id, 'tls_cert tls:endpoint shop.example.com');
  assert.equal(items[1].id, 'tls_cert vault shop.example.com');
  assert.equal(items[2].id, 'tls_cert tls:endpoint shop.example.com 2');
  assert.equal(new Set(items.map((i) => i.id)).size, 3);
});

test('toItems buckets each item by its deadline', () => {
  const items = toItems({
    generatedAt: '',
    count: 0,
    expired: 0,
    items: [raw({ daysLeft: -2 }), raw({ daysLeft: 7 }), raw({ daysLeft: 200 })],
  });
  assert.deepEqual(
    items.map((i) => i.severity),
    ['expired', 'urgent', 'ok'],
  );
});

test('an empty report is a report, not a crash', () => {
  assert.deepEqual(toItems({ generatedAt: '', count: 0, expired: 0, items: [] }), []);
});

test('items sort worst deadline first, then by priority', () => {
  const items = toItems({
    generatedAt: '',
    count: 0,
    expired: 0,
    items: [
      raw({ name: 'calm', daysLeft: 80, priority: 0.9 }),
      raw({ name: 'broken', daysLeft: -1, priority: 0.1 }),
      raw({ name: 'urgent-low', daysLeft: 3, priority: 0.2 }),
      raw({ name: 'urgent-high', daysLeft: 9, priority: 0.8 }),
    ],
  }).sort(compareItems);
  assert.deepEqual(
    items.map((i) => i.name),
    ['broken', 'urgent-high', 'urgent-low', 'calm'],
  );
});

test('parseWarnings picks the failed sources out of stderr', () => {
  const stderr = [
    'expiry-radar: warning: aws: AccessDenied: not authorized to perform acm:ListCertificates',
    'some unrelated chatter',
    '  expiry-radar: warning: vault: 403 permission denied  ',
    'expiry-radar: something that is not a warning',
  ].join('\n');
  assert.deepEqual(parseWarnings(stderr), [
    'aws: AccessDenied: not authorized to perform acm:ListCertificates',
    'vault: 403 permission denied',
  ]);
});

test('parseWarnings finds nothing in a clean run', () => {
  assert.deepEqual(parseWarnings(''), []);
  assert.deepEqual(parseWarnings('all good\n'), []);
});

test('describe names the namespace only when there is one', () => {
  const [plain, namespaced] = toItems({
    generatedAt: '',
    count: 0,
    expired: 0,
    items: [raw(), raw({ name: 'web-tls', namespace: 'prod' })],
  });
  assert.ok(!describe(plain).some((f) => f.startsWith('namespace:')));
  assert.ok(describe(namespaced).includes('namespace: prod'));
  assert.ok(describe(plain).includes('expires: 2026-10-09 (40d)'));
});
