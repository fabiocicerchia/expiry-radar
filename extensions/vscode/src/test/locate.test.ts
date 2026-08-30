import assert from 'node:assert/strict';
import { test } from 'node:test';

import { declaredIn } from '../locate';

const CONFIG = `{
  "endpoints": [
    { "host": "shop.example.com", "labels": { "traffic": "2400", "public": "true" } },
    { "host": "admin.internal.example.com:8443", "labels": { "ingress.class": "nginx-internal" } }
  ],
  "domains": ["example.com", "example.net"],
  "k8s": { "enabled": true, "namespaces": ["prod", "payments"] },
  "overrides": [{ "match": "payments/*", "blastRadius": 1.0 }]
}
`;

test('an endpoint host is placed on the line that declares it', () => {
  const found = declaredIn('/repo/expiry-radar.json', CONFIG);
  const shop = found.get('shop.example.com');
  assert.deepEqual(shop, { file: '/repo/expiry-radar.json', line: 3, column: 15 });
  // The column is the opening quote of the value, so the squiggle covers the
  // host rather than the key that introduces it.
  assert.ok(CONFIG.split('\n')[2].slice(shop!.column - 1).startsWith('"shop.example.com"'));
});

test('a host with a port keeps the port, because the item name does', () => {
  const found = declaredIn('c.json', CONFIG);
  assert.equal(found.get('admin.internal.example.com:8443')?.line, 4);
  assert.equal(found.has('admin.internal.example.com'), false);
});

test('domains are placed individually, not as one array', () => {
  const found = declaredIn('c.json', CONFIG);
  assert.equal(found.get('example.com')?.line, 6);
  assert.equal(found.get('example.net')?.line, 6);
  assert.ok(found.get('example.net')!.column > found.get('example.com')!.column);
});

test('only the two declared arrays are scanned', () => {
  const found = declaredIn('c.json', CONFIG);
  // A k8s namespace and an override glob are strings in arrays too, and
  // neither declares an item that could be squiggled.
  assert.equal(found.has('prod'), false);
  assert.equal(found.has('payments/*'), false);
});

test('a brace inside a value cannot close the array early', () => {
  const config = `{
  "endpoints": [
    { "host": "a.example.com", "labels": { "note": "]}" } },
    { "host": "b.example.com" }
  ]
}`;
  const found = declaredIn('c.json', config);
  assert.equal(found.get('a.example.com')?.line, 3);
  assert.equal(found.get('b.example.com')?.line, 4);
});

test('a config being typed claims nothing rather than guessing', () => {
  const found = declaredIn('c.json', '{ "endpoints": [ { "host": "a.example.com" }');
  assert.equal(found.size, 0);
});

test('a config with neither array is not an error', () => {
  assert.equal(declaredIn('c.json', '{ "aws": { "enabled": true } }').size, 0);
  assert.equal(declaredIn('c.json', 'not json at all').size, 0);
});

test('the first declaration of a duplicated host wins', () => {
  const config = `{
  "endpoints": [
    { "host": "a.example.com" },
    { "host": "a.example.com" }
  ]
}`;
  assert.equal(declaredIn('c.json', config).get('a.example.com')?.line, 3);
});

test('an escaped character in a host round-trips to the name the CLI reports', () => {
  const found = declaredIn('c.json', '{"domains": ["ex\\u0061mple.com"]}');
  assert.ok(found.has('example.com'));
});
