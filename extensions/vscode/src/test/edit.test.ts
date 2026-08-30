import assert from 'node:assert/strict';
import { test } from 'node:test';

import { addToArray, invalidExpires, renderEntry } from '../edit';

/** Every result has to still be a config the CLI can load. */
function reparse(text: string): Record<string, unknown> {
  return JSON.parse(text) as Record<string, unknown>;
}

test('an entry is appended to a multi-line array, indented like its siblings', () => {
  const before = `{
  "endpoints": [
    { "host": "shop.example.com" }
  ]
}
`;
  const { text, line } = addToArray(before, 'endpoints', renderEntry('endpoint', 'api.example.com'));
  assert.equal(
    text,
    `{
  "endpoints": [
    { "host": "shop.example.com" },
    {"host":"api.example.com"}
  ]
}
`,
  );
  assert.equal(line, 4);
  assert.equal((reparse(text).endpoints as unknown[]).length, 2);
});

test('a one-line array stays a one-line array', () => {
  // Re-flowing `["a", "b"]` across three lines to add a third is not the diff
  // anybody asked for.
  const before = `{ "domains": ["example.com", "example.net"] }`;
  const { text } = addToArray(before, 'domains', renderEntry('domain', 'example.org'));
  assert.equal(text, `{ "domains": ["example.com", "example.net", "example.org"] }`);
  assert.deepEqual(reparse(text).domains, ['example.com', 'example.net', 'example.org']);
});

test('an empty array takes the first entry without a stray comma', () => {
  const { text } = addToArray(`{ "domains": [] }`, 'domains', renderEntry('domain', 'a.example'));
  assert.equal(text, `{ "domains": ["a.example"] }`);
  const multi = addToArray(`{\n  "domains": [\n  ]\n}\n`, 'domains', renderEntry('domain', 'a.example'));
  assert.deepEqual(reparse(multi.text).domains, ['a.example']);
});

test('a missing key is added to the object rather than replacing it', () => {
  const before = `{
  "endpoints": [
    { "host": "shop.example.com" }
  ]
}
`;
  const { text } = addToArray(before, 'manual', renderEntry('manual', {
    name: 'acme-corp.co.uk',
    kind: 'domain',
    expires: '2027-03-01',
  }));
  const parsed = reparse(text);
  // The existing key survives — this is the case where a naive rewrite loses
  // everything the operator already had.
  assert.equal((parsed.endpoints as unknown[]).length, 1);
  assert.deepEqual(parsed.manual, [
    { name: 'acme-corp.co.uk', kind: 'domain', expires: '2027-03-01' },
  ]);
});

test('an empty or absent config becomes a config', () => {
  for (const before of ['', '   \n', '{}', '{\n}\n']) {
    const { text } = addToArray(before, 'domains', renderEntry('domain', 'a.example'));
    assert.deepEqual(reparse(text).domains, ['a.example'], `from ${JSON.stringify(before)}`);
  }
});

test('four-space and tab indentation are preserved', () => {
  const spaces = `{\n    "domains": [\n        "a.example"\n    ]\n}\n`;
  assert.ok(addToArray(spaces, 'domains', '"b.example"').text.includes('\n        "b.example"'));
  const tabs = `{\n\t"domains": [\n\t\t"a.example"\n\t]\n}\n`;
  assert.ok(addToArray(tabs, 'domains', '"b.example"').text.includes('\n\t\t"b.example"'));
});

test('a bracket inside a value does not confuse the append', () => {
  const before = `{
  "endpoints": [
    { "host": "a.example", "labels": { "note": "]}" } }
  ],
  "domains": ["x.example"]
}
`;
  const { text } = addToArray(before, 'endpoints', renderEntry('endpoint', 'b.example'));
  const parsed = reparse(text);
  assert.equal((parsed.endpoints as unknown[]).length, 2);
  assert.deepEqual(parsed.domains, ['x.example']);
});

test('the reported line is where the entry actually landed', () => {
  const before = `{
  "domains": [
    "a.example"
  ]
}
`;
  const { text, line } = addToArray(before, 'domains', '"b.example"');
  assert.match(text.split('\n')[line - 1], /b\.example/);
});

test('a manual entry carries what ranking needs, and omits what it does not', () => {
  const full = renderEntry('manual', {
    name: 'code-signing',
    kind: 'tls_cert',
    expires: '2026-11-15',
    namespace: 'release',
  });
  assert.deepEqual(JSON.parse(full), {
    name: 'code-signing',
    kind: 'tls_cert',
    expires: '2026-11-15',
    namespace: 'release',
  });
  // An empty namespace is left out rather than written as "", which would show
  // up as a "/name" prefix in every report.
  const bare = renderEntry('manual', { name: 'a', kind: 'secret', expires: '2026-11-15', namespace: '  ' });
  assert.equal('namespace' in JSON.parse(bare), false);
});

test('entries are trimmed, so a pasted hostname does not become a name with a space', () => {
  assert.equal(renderEntry('domain', '  example.com \n'), '"example.com"');
  assert.deepEqual(JSON.parse(renderEntry('endpoint', ' shop.example.com ')), {
    host: 'shop.example.com',
  });
});

test('the date check accepts what the CLI accepts', () => {
  assert.equal(invalidExpires('2027-03-01'), undefined);
  assert.equal(invalidExpires('2027-03-01T15:04:05Z'), undefined);
  assert.equal(invalidExpires('2027-03-01T15:04:05+02:00'), undefined);
});

test('the date check rejects what the CLI would reject at load', () => {
  assert.ok(invalidExpires(''));
  assert.ok(invalidExpires('next march'));
  assert.ok(invalidExpires('03/01/2027'));
  // Date.parse rolls this over to 3 March rather than failing, which would
  // silently record the wrong deadline.
  assert.ok(invalidExpires('2027-02-31'));
});
