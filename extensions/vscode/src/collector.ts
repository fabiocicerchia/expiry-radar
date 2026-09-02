/**
 * One collection, from the process to the panel.
 *
 * Running expiry-radar is `runner.ts`; deciding when to run it is
 * `scheduler.ts`. This is what happens in between and afterwards: place the
 * items on the lines that asked for them, keep the snapshot, publish the
 * diagnostics, repaint — and say something useful when a run fails or comes
 * back short of a source.
 */
import * as fs from 'fs';
import * as vscode from 'vscode';

import { DiagnosticGroup, DiagnosticPublisher } from './diagnostics';
import { primaryFolder, settingsFor } from './folder';
import { InventoryView } from './inventoryView';
import { declaredIn } from './locate';
import { log } from './log';
import { toItems } from './parse';
import { collect, promptInstall, RadarNotFoundError } from './runner';
import { Job } from './scheduler';
import { StatusBar } from './status';
import { ResultStore } from './store';
import { Item, Snapshot } from './types';

/** A broken binary must not produce a popup every hour. */
const ERROR_COOLDOWN_MS = 60_000;

/**
 * The sources whose items were *recorded* in the config file, and so have a line
 * to point at and an entry to remove. Everything else was discovered — a
 * certificate on an Ingress was never written down here, so squiggling a config
 * line for it would be a lie, and offering to delete it would be worse.
 */
const DECLARED_BY = new Set(['tls:endpoint', 'domain:rdap', 'domain:whois', 'manual']);

export class Collector {
  private lastErrorAt = 0;
  private lastWarningAt = 0;
  private notFoundShown = false;
  /** Label of the collection in flight, if any — the status bar belongs to it. */
  private collecting?: string;

  constructor(
    private readonly store: ResultStore,
    private readonly diagnostics: DiagnosticPublisher,
    private readonly statusBar: StatusBar,
    private readonly inventoryView: InventoryView,
  ) {}

  /** Repaint the panel and the status bar from whatever the store now holds. */
  paint(): void {
    const folder = primaryFolder();
    this.inventoryView.refresh();
    if (this.collecting !== undefined) return; // The run owns the status bar.
    const s = settingsFor(folder);
    this.statusBar.idle(folder ? this.store.get(folder) : undefined, s.statusWarnWithinDays);
  }

  publishDiagnostics(): void {
    const groups: DiagnosticGroup[] = this.store
      .folders()
      .map((f) => ({ items: this.store.items(f), settings: settingsFor(f) }));
    this.diagnostics.publish(groups);
  }

  /** The settings changed: look for the binary again, and re-offer to install it. */
  forgetNotFound(): void {
    this.notFoundShown = false;
  }

  async execute(job: Job, token: vscode.CancellationToken): Promise<void> {
    const s = settingsFor(job.folder);
    this.collecting = job.folder.name;
    this.inventoryView.setCollecting(job.folder.name);
    this.statusBar.collecting(job.folder.name);
    try {
      const { report, result } = await collect({ folder: job.folder, reason: job.reason }, s, token);
      const items = await withOrigins(
        toItems(report, s.warnWithinDays, s.infoWithinDays),
        result.configPath,
      );
      const snapshot: Snapshot = {
        items,
        warnings: result.warnings,
        generatedAt: report.generatedAt,
        configPath: result.configPath,
        at: Date.now(),
        durationMs: result.durationMs,
      };
      this.store.set(job.folder, snapshot);
      this.publishDiagnostics();
      if (result.warnings.length) this.announceWarnings(result.warnings, job);
    } catch (err) {
      this.reportFailure(err, job);
    } finally {
      this.collecting = undefined;
      this.inventoryView.setCollecting('');
      this.paint();
    }
  }

  reportFailure(err: unknown, job: Job): void {
    if (err instanceof vscode.CancellationError) {
      log().info(`collection cancelled: ${job.reason}`);
      return;
    }
    if (err instanceof RadarNotFoundError) {
      log().error(err.message);
      if (this.notFoundShown) return;
      this.notFoundShown = true;
      void promptInstall(err.message);
      return;
    }
    const message = err instanceof Error ? err.message : String(err);
    log().error(message);
    // Automatic runs fail quietly after the first notification.
    if (job.manual || Date.now() - this.lastErrorAt > ERROR_COOLDOWN_MS) {
      this.lastErrorAt = Date.now();
      void vscode.window
        .showErrorMessage(`expiry-radar: ${message}`, 'Show log')
        .then((choice) => {
          if (choice === 'Show log') log().show(true);
        });
    }
  }

  /**
   * A source that failed is not a log line. The whole product is a complete
   * inventory ranked by consequence, and a run that lost a source produces a
   * report that looks exactly like a clean estate.
   */
  announceWarnings(warnings: string[], job: Job): void {
    if (!job.manual && Date.now() - this.lastWarningAt < ERROR_COOLDOWN_MS) return;
    this.lastWarningAt = Date.now();
    void vscode.window
      .showWarningMessage(
        `expiry-radar: ${warnings.length} source(s) failed — this inventory is incomplete.`,
        'Show log',
        'Check environment',
      )
      .then((choice) => {
        if (choice === 'Show log') log().show(true);
        else if (choice === 'Check environment') void vscode.commands.executeCommand('expiryRadar.checkEnvironment');
      });
  }
}

/**
 * Attach each item to the line that asked for it. Read fresh per run rather
 * than cached: the config is a file somebody edits, and a stale map would put
 * a squiggle on the wrong line — worse than none at all.
 */
async function withOrigins(items: Item[], configPath: string): Promise<Item[]> {
  if (!configPath) return items;
  let declared: Map<string, { file: string; line: number; column: number }>;
  try {
    declared = declaredIn(configPath, await fs.promises.readFile(configPath, 'utf8'));
  } catch (err) {
    log().debug(`could not read ${configPath} to place items: ${String(err)}`);
    return items;
  }
  return items.map((item) => {
    if (!DECLARED_BY.has(item.source)) return item;
    const origin = declared.get(item.name);
    return origin ? { ...item, origin } : item;
  });
}
