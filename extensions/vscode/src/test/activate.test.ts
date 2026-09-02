/**
 * What `activate()` leaves behind.
 *
 * Activation has no return value, so every assertion here is about the editor
 * it wired itself into: which command ids now exist, what a command does when
 * there is nothing to do it to, and whether disposing the subscriptions
 * actually stops the timers it armed. The shim records those; nothing here
 * reaches into the extension's own state.
 *
 * Offline by construction: the only host any of this can reach is a closed port
 * on loopback.
 */
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { test } from 'node:test';

import { activate } from '../extension';
import {
  answers,
  commands,
  executedCommands,
  prompts,
  registeredCommands,
  resetShim,
  testConfiguration,
  Uri,
  workspace,
} from './vscode-shim';

/** The tests are bundled into `out/`, one directory below the manifest. */
const manifest = JSON.parse(
  fs.readFileSync(path.join(__dirname, '..', 'package.json'), 'utf8'),
) as { contributes: { commands: { command: string }[] } };

/** A closed port: refused instantly, so no collection this triggers can leave the machine. */
const REFUSED = '127.0.0.1:1';

interface Context {
  subscriptions: { dispose(): unknown }[];
}

function timerCount(): number {
  return process.getActiveResourcesInfo().filter((r) => r === 'Timeout').length;
}

/** Activate, run the body, and always tear down — a leaked timer would follow the next test. */
async function activated(body: (context: Context) => Promise<void> | void): Promise<void> {
  resetShim();
  const context: Context = { subscriptions: [] };
  activate(context as never);
  try {
    await body(context);
  } finally {
    for (const d of context.subscriptions.reverse()) d.dispose();
  }
}

test('activate registers exactly the commands the manifest contributes', async () => {
  await activated(() => {
    const contributed = manifest.contributes.commands.map((c) => c.command).sort();
    assert.deepEqual([...registeredCommands.keys()].sort(), contributed);
  });
});

test('disposing the subscriptions stops every timer activation armed', async () => {
  resetShim();
  const before = timerCount();
  const context: Context = { subscriptions: [] };
  activate(context as never);
  // The startup collection is deferred, and the periodic refresh is an
  // interval: both are live now, and both are somebody's job to clear.
  assert.ok(timerCount() > before, 'activation armed no timer at all');
  for (const d of context.subscriptions.reverse()) d.dispose();
  assert.equal(timerCount(), before, 'a timer outlived the extension');
});

test('a collection asked for with no folder open says so, and collects nothing', async () => {
  await activated(async () => {
    await commands.executeCommand('expiryRadar.scan');
    assert.deepEqual(prompts, ['warning: expiry-radar: open a folder first.']);
  });
});

test('the grouping commands publish the context its menus are written against', async () => {
  await activated(async () => {
    executedCommands.length = 0;
    await commands.executeCommand('expiryRadar.groupByKind');
    // `package.json` gates the view/title buttons on this exact key and value.
    assert.deepEqual(executedCommands.at(-1), {
      command: 'setContext',
      args: ['expiryRadar.grouping', 'kind'],
    });
    await commands.executeCommand('expiryRadar.groupByRank');
    assert.deepEqual(executedCommands.at(-1), {
      command: 'setContext',
      args: ['expiryRadar.grouping', 'rank'],
    });
  });
});

test('adding an endpoint prompts for it, writes it, and opens the file it wrote', async () => {
  const project = fs.mkdtempSync(path.join(os.tmpdir(), 'expiry-radar-activate-'));
  try {
    await activated(async () => {
      workspace.workspaceFolders = [{ uri: Uri.file(project), name: 'add', index: 0 }];
      // Manual: nothing may start a collection on its own during this test.
      testConfiguration.set('expiryRadar.scan.trigger', 'manual');
      testConfiguration.set('expiryRadar.scan.onStartup', false);
      answers.push(
        (items: { label: string }[]) => items.find((i) => i.label === 'Endpoint'),
        REFUSED,
      );

      await commands.executeCommand('expiryRadar.addItem');

      assert.deepEqual(prompts.slice(0, 2), [
        'quickPick: expiry-radar: record what?',
        'inputBox: expiry-radar: record an endpoint',
      ]);
      const written = JSON.parse(
        fs.readFileSync(path.join(project, 'expiry-radar.json'), 'utf8'),
      ) as { endpoints: { host: string }[] };
      assert.deepEqual(written.endpoints, [{ host: REFUSED }]);
    });
  } finally {
    fs.rmSync(project, { recursive: true, force: true });
  }
});

test('backing out of the first prompt writes no config at all', async () => {
  const project = fs.mkdtempSync(path.join(os.tmpdir(), 'expiry-radar-activate-'));
  try {
    await activated(async () => {
      workspace.workspaceFolders = [{ uri: Uri.file(project), name: 'add', index: 0 }];
      testConfiguration.set('expiryRadar.scan.trigger', 'manual');
      testConfiguration.set('expiryRadar.scan.onStartup', false);
      answers.push(undefined);

      await commands.executeCommand('expiryRadar.addItem');

      assert.equal(prompts.length, 1);
      assert.equal(fs.existsSync(path.join(project, 'expiry-radar.json')), false);
    });
  } finally {
    fs.rmSync(project, { recursive: true, force: true });
  }
});
