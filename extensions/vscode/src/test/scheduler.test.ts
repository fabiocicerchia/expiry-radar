import assert from 'node:assert/strict';
import { test } from 'node:test';

import { Job, Scheduler } from '../scheduler';
import { Uri } from './vscode-shim';

const folder = { uri: Uri.file('/repo'), name: 'repo', index: 0 } as unknown as Job['folder'];

const job = (reason: string, manual = false): Job => ({ folder, reason, manual });

const settings = () => ({ debounceMs: 5, intervalMinutes: 60 });

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

test('a burst of requests collapses into one run, and the newest wins', async () => {
  const ran: string[] = [];
  const scheduler = new Scheduler(async (j) => {
    ran.push(j.reason);
  }, settings);

  scheduler.schedule(job('first'));
  scheduler.schedule(job('second'));
  scheduler.schedule(job('third'));
  await sleep(40);

  assert.deepEqual(ran, ['third']);
  scheduler.dispose();
});

test('an automatic request never displaces a queued manual one', async () => {
  const ran: string[] = [];
  const scheduler = new Scheduler(async (j) => {
    ran.push(j.reason);
  }, settings);

  scheduler.schedule(job('manual', true));
  scheduler.schedule(job('automatic'));
  await sleep(40);

  assert.deepEqual(ran, ['manual']);
  scheduler.dispose();
});

test('only one collection runs at a time', async () => {
  let inFlight = 0;
  let peak = 0;
  const scheduler = new Scheduler(async () => {
    inFlight += 1;
    peak = Math.max(peak, inFlight);
    await sleep(20);
    inFlight -= 1;
  }, settings);

  const first = scheduler.runNow(job('one', true));
  const second = scheduler.runNow(job('two', true));
  await Promise.all([first, second]);

  assert.equal(peak, 1);
  scheduler.dispose();
});

test('a manual run cancels an automatic one already in flight', async () => {
  const cancelled: string[] = [];
  const scheduler = new Scheduler(async (j, token) => {
    token.onCancellationRequested(() => cancelled.push(j.reason));
    await sleep(30);
  }, settings);

  const automatic = scheduler.runNow(job('automatic'));
  await sleep(5);
  const manual = scheduler.runNow(job('manual', true));
  await Promise.all([automatic, manual]);

  assert.deepEqual(cancelled, ['automatic']);
  scheduler.dispose();
});

test('a failed run does not poison the queue', async () => {
  const ran: string[] = [];
  const scheduler = new Scheduler(async (j) => {
    ran.push(j.reason);
    if (j.reason === 'boom') throw new Error('boom');
  }, settings);

  await scheduler.runNow(job('boom', true)).catch(() => undefined);
  await scheduler.runNow(job('after', true));

  assert.deepEqual(ran, ['boom', 'after']);
  scheduler.dispose();
});

test('cancel drops what is queued as well as what is running', async () => {
  const ran: string[] = [];
  const scheduler = new Scheduler(async (j) => {
    ran.push(j.reason);
  }, settings);

  scheduler.schedule(job('queued'));
  scheduler.cancel();
  await sleep(40);

  assert.deepEqual(ran, []);
  scheduler.dispose();
});
