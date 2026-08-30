/**
 * When collections are allowed to happen.
 *
 * A collection dials every configured host over TLS, queries RDAP for every
 * domain and calls the Kubernetes, Vault and AWS APIs. Registries rate-limit
 * RDAP, and a laptop with an editor open all day would happily spend the
 * afternoon hammering them. So every trigger funnels through here:
 *
 *  - **debounce** — a burst of config saves collapses into one run;
 *  - **single flight** — exactly one expiry-radar process at a time, ever;
 *  - **coalescing** — while one runs, only the newest request survives;
 *  - **preemption** — a manual run cancels an automatic one already in flight;
 *  - **focus gate** — the periodic refresh skips while the window is in the
 *    background, and skips again if a run already landed recently.
 */
import * as vscode from 'vscode';

import { log } from './log';

export interface Job {
  folder: vscode.WorkspaceFolder;
  /** Why this run started — shown in the log and the progress notification. */
  reason: string;
  /** User-initiated: preempts an automatic run and ignores the focus gate. */
  manual: boolean;
}

export class Scheduler {
  private timer?: NodeJS.Timeout;
  private sweep?: NodeJS.Timeout;
  private pending?: Job;
  private active?: { job: Job; source: vscode.CancellationTokenSource };
  private lastCompletedAt = 0;
  /** Serializes every run, so `runNow` resolves when *its* job is done. */
  private chain: Promise<void> = Promise.resolve();

  constructor(
    private readonly execute: (job: Job, token: vscode.CancellationToken) => Promise<void>,
    private readonly settings: () => { debounceMs: number; intervalMinutes: number },
  ) {}

  /** Queue a job behind the debounce timer. The newest request wins. */
  schedule(job: Job): void {
    if (this.pending && this.pending.manual && !job.manual) return;
    this.pending = job;
    this.arm(this.settings().debounceMs);
  }

  /**
   * Run now: cancels an automatic run already in flight. The returned promise
   * resolves when *this* job is done, so a caller that needs the result
   * (opening the report) can await it.
   */
  runNow(job: Job): Promise<void> {
    if (this.active && !this.active.job.manual) {
      log().info(`preempting ${this.active.job.reason} for ${job.reason}`);
      this.active.source.cancel();
    }
    return this.start(job);
  }

  cancel(): void {
    clearTimeout(this.timer);
    this.timer = undefined;
    this.pending = undefined;
    this.active?.source.cancel();
  }

  cancelJob(job: Job): void {
    if (this.active?.job === job) this.active.source.cancel();
    if (this.pending === job) this.pending = undefined;
  }

  /** (Re)arm the periodic refresh. `factory` returns undefined when there is nothing to refresh. */
  setSweep(enabled: boolean, factory: () => Job | undefined): void {
    clearInterval(this.sweep);
    this.sweep = undefined;
    if (!enabled) return;
    const periodMs = this.settings().intervalMinutes * 60_000;
    this.sweep = setInterval(() => {
      if (!vscode.window.state.focused) {
        log().debug('refresh skipped — window not focused');
        return;
      }
      if (Date.now() - this.lastCompletedAt < periodMs * 0.9) return; // Already fresh.
      const job = factory();
      if (job) this.schedule(job);
    }, periodMs);
  }

  private arm(delayMs: number): void {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.fire(), Math.max(0, delayMs));
  }

  private async fire(): Promise<void> {
    const job = this.pending;
    if (!job) return;
    if (this.active) return; // Picked up when the current run finishes.

    this.pending = undefined;
    await this.start(job);
  }

  private start(job: Job): Promise<void> {
    const run = this.chain.then(async () => {
      const source = new vscode.CancellationTokenSource();
      this.active = { job, source };
      try {
        await this.execute(job, source.token);
      } finally {
        source.dispose();
        this.active = undefined;
        this.lastCompletedAt = Date.now();
        if (this.pending) this.arm(0);
      }
    });
    // The chain must survive a failed run, or every later job inherits the
    // rejection and nothing ever runs again.
    this.chain = run.catch(() => undefined);
    return run;
  }

  dispose(): void {
    clearTimeout(this.timer);
    clearInterval(this.sweep);
    this.active?.source.cancel();
  }
}
