import assert from 'node:assert/strict';
import { test } from 'node:test';

import { addToArray, arrayForSource, invalidExpires, removeEntry, renderEntry } from '../edit';

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

test('removing the middle of a multi-line array leaves valid JSON', () => {
  const before = `{
  "endpoints": [
    { "host": "a.example" },
    { "host": "b.example" },
    { "host": "c.example" }
  ]
}
`;
  // Line 4, at the host value — exactly what locate.ts reports for that row.
  const text = removeEntry(before, 'endpoints', 4, 15)!;
  assert.deepEqual(reparse(text).endpoints, [{ host: 'a.example' }, { host: 'c.example' }]);
  // No blank line where the entry used to be.
  assert.ok(!/\n\s*\n/.test(text), text);
});

test('removing the last element takes the comma before it, not after', () => {
  const before = `{
  "endpoints": [
    { "host": "a.example" },
    { "host": "b.example" }
  ]
}
`;
  const text = removeEntry(before, 'endpoints', 4, 15)!;
  assert.deepEqual(reparse(text).endpoints, [{ host: 'a.example' }]);
});

test('removing the only element leaves an empty array, not a broken one', () => {
  const text = removeEntry(`{\n  "domains": [\n    "a.example"\n  ]\n}\n`, 'domains', 3, 5)!;
  assert.deepEqual(reparse(text).domains, []);
});

test('an entry spanning several lines is removed whole', () => {
  const before = `{
  "manual": [
    {
      "name": "code-signing",
      "kind": "tls_cert",
      "expires": "2026-11-15"
    },
    { "name": "other", "kind": "secret", "expires": "2027-01-01" }
  ]
}
`;
  // The origin points at the name value on line 4, inside the element.
  const text = removeEntry(before, 'manual', 4, 15)!;
  const parsed = reparse(text) as { manual: { name: string }[] };
  assert.deepEqual(parsed.manual.map((m) => m.name), ['other']);
});

test('a brace inside a value does not end the element early', () => {
  const before = `{
  "endpoints": [
    { "host": "a.example", "labels": { "note": "}]" } },
    { "host": "b.example" }
  ]
}
`;
  const text = removeEntry(before, 'endpoints', 3, 15)!;
  assert.deepEqual(reparse(text).endpoints, [{ host: 'b.example' }]);
});

test('a one-line config loses one entry, not the whole document', () => {
  // Addressing by line alone would take the document's own opening brace here
  // and delete everything. The column is what separates the two entries.
  const before = `{ "domains": ["a.example", "b.example"] }`;
  assert.deepEqual(reparse(removeEntry(before, 'domains', 1, 15)!).domains, ['b.example']);
  assert.deepEqual(reparse(removeEntry(before, 'domains', 1, 28)!).domains, ['a.example']);
});

test('a position naming no entry is refused rather than guessed at', () => {
  const before = `{\n  "domains": [\n    "a.example"\n  ]\n}\n`;
  // Past the end of the file, and outside the array — the file was edited
  // since the collection that reported this position.
  assert.equal(removeEntry(before, 'domains', 999, 1), undefined);
  assert.equal(removeEntry(before, 'domains', 1, 1), undefined);
  assert.equal(removeEntry(before, 'endpoints', 3, 5), undefined);
});

test('removal never reaches outside the array it was given', () => {
  const before = `{
  "endpoints": [{ "host": "a.example" }],
  "domains": ["keep.example"],
  "manual": [{ "name": "keep", "kind": "secret", "expires": "2027-01-01" }]
}
`;
  const text = removeEntry(before, 'endpoints', 2, 27)!;
  const parsed = reparse(text);
  assert.deepEqual(parsed.endpoints, []);
  assert.deepEqual(parsed.domains, ['keep.example']);
  assert.equal((parsed.manual as unknown[]).length, 1);
});

test('a source maps to the array it records into, and discovery maps to none', () => {
  assert.equal(arrayForSource('tls:endpoint'), 'endpoints');
  assert.equal(arrayForSource('domain:rdap'), 'domains');
  assert.equal(arrayForSource('domain:whois'), 'domains');
  assert.equal(arrayForSource('manual'), 'manual');
  // Discovered: there is no config entry to delete, and offering to would
  // imply this tool writes to your estate.
  for (const discovered of ['k8s:secret', 'aws:acm', 'aws:iam', 'vault:pki_int', 'tls:chain']) {
    assert.equal(arrayForSource(discovered), undefined, discovered);
  }
});
