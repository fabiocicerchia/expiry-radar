import assert from 'node:assert/strict';
import { test } from 'node:test';

import { ResultStore } from '../store';
import { Item, Snapshot } from '../types';
import { Uri, workspace } from './vscode-shim';

const repo = { uri: Uri.file('/repo'), name: 'repo', index: 0 };
const other = { uri: Uri.file('/other'), name: 'other', index: 1 };

type Folder = Parameters<ResultStore['set']>[0];

function item(name: string): Item {
  return {
    id: name,
    display: name,
    severity: 'ok',
    priority: 0.5,
    blastRadius: 0.5,
    daysLeft: 40,
    expired: false,
    kind: 'tls_cert',
    name,
    source: 'tls:endpoint',
    expires: '2026-10-09T12:00:00Z',
    why: 'public endpoint',
  };
}

function snapshot(names: string[], warnings: string[] = []): Snapshot {
  return {
    items: names.map(item),
    warnings,
    generatedAt: '2026-08-30T00:00:00Z',
    configPath: '/repo/expiry-radar.json',
    at: Date.now(),
    durationMs: 1200,
  };
}

test('a newer collection replaces the previous one outright', () => {
  const store = new ResultStore();
  store.set(repo as unknown as Folder, snapshot(['a.example.com', 'b.example.com']));
  store.set(repo as unknown as Folder, snapshot(['a.example.com']));
  // Keeping the row the newer run did not return would be inventing an estate:
  // the source was asked, and it no longer has it.
  assert.deepEqual(
    store.items(repo as unknown as Folder).map((i) => i.name),
    ['a.example.com'],
  );
  store.dispose();
});

test('a folder that has never been collected has no items and no snapshot', () => {
  const store = new ResultStore();
  assert.equal(store.get(repo as unknown as Folder), undefined);
  assert.deepEqual(store.items(repo as unknown as Folder), []);
  store.dispose();
});

test('folders are the collected ones, in workspace order', () => {
  const store = new ResultStore();
  workspace.workspaceFolders = [repo, other];
  store.set(other as unknown as Folder, snapshot(['x.example.com']));
  assert.deepEqual(
    store.folders().map((f) => f.name),
    ['other'],
  );
  store.set(repo as unknown as Folder, snapshot(['y.example.com']));
  assert.deepEqual(
    store.folders().map((f) => f.name),
    ['repo', 'other'],
  );
  workspace.workspaceFolders = [];
  store.dispose();
});

test('failed sources survive on the snapshot, not just in the log', () => {
  const store = new ResultStore();
  store.set(repo as unknown as Folder, snapshot(['a.example.com'], ['aws: AccessDenied']));
  assert.deepEqual(store.get(repo as unknown as Folder)?.warnings, ['aws: AccessDenied']);
  store.dispose();
});

test('every mutation fires exactly one change', () => {
  const store = new ResultStore();
  let fired = 0;
  store.onDidChange(() => (fired += 1));
  store.set(repo as unknown as Folder, snapshot(['a.example.com']));
  store.clear();
  assert.equal(fired, 2);
  assert.deepEqual(store.items(repo as unknown as Folder), []);
  store.dispose();
});
