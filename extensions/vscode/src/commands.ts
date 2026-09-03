/**
 * What each `expiryRadar.*` command does.
 *
 * One method per command, and one table naming the id each is registered
 * under — the ids are the extension's public surface, the same strings
 * `package.json` contributes, so they live in one readable list rather than
 * scattered through the activation sequence.
 */
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';

import { Collector } from './collector';
import { arrayForSource, ARRAY_FOR, addToArray, removeEntry } from './edit';
import { runDoctor } from './doctor';
import { primaryFolder, settingsFor } from './folder';
import { InventoryView, Node } from './inventoryView';
import { log } from './log';
import { describe, humanDays, toItems } from './parse';
import { pickEntryKind, pickExportFormat, promptEntry, promptHost } from './prompts';
import { ReportView } from './report';
import { collect, Format, hasSources, resolveConfig, runRadar } from './runner';
import { Job, Scheduler } from './scheduler';
import { ResultStore } from './store';

export class Commands {
  /** When the report in the tab was rendered, so a stale one is replaced. */
  private reportRenderedAt = 0;

  constructor(
    private readonly collector: Collector,
    private readonly scheduler: Scheduler,
    private readonly reportView: ReportView,
    private readonly inventoryView: InventoryView,
    private readonly store: ResultStore,
  ) {}

  /** Every command id this extension answers to, and what answers it. */
  register(): vscode.Disposable[] {
    const handlers: Record<string, (...args: never[]) => unknown> = {
      'expiryRadar.scan': () => this.run('command'),
      'expiryRadar.showReport': () => this.openReport(),
      'expiryRadar.exportReport': () => this.exportReport(),
      'expiryRadar.probeHost': () => this.probeHost(),
      'expiryRadar.addItem': () => this.addItem(),
      'expiryRadar.openConfig': () => this.openConfig(),
      'expiryRadar.cancel': () => this.scheduler.cancel(),
      'expiryRadar.showLog': () => log().show(true),
      'expiryRadar.filterItems': () => this.inventoryView.pickFilters(),
      'expiryRadar.groupByKind': () => this.inventoryView.setGrouping('kind'),
      'expiryRadar.groupByRank': () => this.inventoryView.setGrouping('rank'),
      'expiryRadar.expandAll': () => this.inventoryView.expandAll(),
      'expiryRadar.removeItem': (node?: Node) => this.removeItem(node),
      'expiryRadar.copyItem': (node?: Node) => this.copyItem(node),
      'expiryRadar.checkEnvironment': () => this.checkEnvironment(),
    };
    return Object.entries(handlers).map(([id, handler]) =>
      vscode.commands.registerCommand(id, handler),
    );
  }

  /** A manual collection, with a cancellable progress notification. */
  async run(reason: string): Promise<boolean> {
    const folder = primaryFolder();
    if (!folder) {
      void vscode.window.showWarningMessage('expiry-radar: open a folder first.');
      return false;
    }
    if (!hasSources(folder, settingsFor(folder))) {
      await this.offerToConfigure();
      return false;
    }
    const job: Job = { folder, reason, manual: true };
    await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `expiry-radar: collecting ${folder.name}`,
        cancellable: true,
      },
      (_progress, token) => {
        token.onCancellationRequested(() => this.scheduler.cancelJob(job));
        return this.scheduler.runNow(job);
      },
    );
    return this.store.get(folder) !== undefined;
  }

  /**
   * The CLI would exit 2 saying exactly this. Say it here with the thing that
   * fixes it attached, rather than as a stack of shell output.
   */
  private async offerToConfigure(): Promise<void> {
    const choice = await vscode.window.showWarningMessage(
      'expiry-radar: no sources configured — every source is opt-in, so there is nothing to collect.',
      'Open config file',
      'Open settings',
    );
    if (choice === 'Open config file') await this.openConfig();
    else if (choice === 'Open settings') {
      await vscode.commands.executeCommand('workbench.action.openSettings', 'expiryRadar');
    }
  }

  /**
   * One `-format <fmt>` run, outside the scheduler's single-flight lane on
   * purpose: it produces a document, not the board, and making the report wait
   * behind a periodic refresh would be a spinner for no reason.
   */
  private async render(format: Format, title: string): Promise<string | undefined> {
    const folder = primaryFolder();
    if (!folder) {
      void vscode.window.showWarningMessage('expiry-radar: open a folder first.');
      return undefined;
    }
    const s = settingsFor(folder);
    return vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title, cancellable: true },
      async (_progress, token) => {
        try {
          const result = await runRadar({ folder, format, reason: title }, s, token);
          if (result.warnings.length) {
            this.collector.announceWarnings(result.warnings, { folder, reason: title, manual: true });
          }
          return result.stdout;
        } catch (err) {
          this.collector.reportFailure(err, { folder, reason: title, manual: true });
          return undefined;
        }
      },
    );
  }

  async openReport(): Promise<void> {
    const folder = primaryFolder();
    if (!folder) return;
    // A report older than the newest collection would show a different estate
    // from the panel next to it.
    const stale = (this.store.get(folder)?.at ?? 0) > this.reportRenderedAt;
    if (stale || !this.reportView.current) {
      // The CLI renders one format per invocation, so the report is its own
      // collection — this dials the estate a second time, which is why it is
      // only ever done on demand and never on a background refresh.
      const html = await this.render('html', 'expiry-radar: rendering the report');
      if (!html) return;
      this.reportView.current = html;
      this.reportRenderedAt = Date.now();
    }
    this.reportView.show(this.reportView.current, folder.name);
  }

  async exportReport(): Promise<void> {
    const folder = primaryFolder();
    if (!folder) return;
    const choice = await pickExportFormat();
    if (!choice) return;

    const body = await this.render(choice.format, `expiry-radar: rendering ${choice.label}`);
    if (body === undefined) return;

    const stamp = new Date().toISOString().slice(0, 10);
    const target = await vscode.window.showSaveDialog({
      defaultUri: vscode.Uri.file(
        path.join(folder.uri.fsPath || os.homedir(), `expiry-radar-${stamp}.${choice.ext}`),
      ),
      filters: { [choice.label]: [choice.ext] },
      title: 'Export the expiry-radar report',
    });
    if (!target) return;
    await fs.promises.writeFile(target.fsPath, body, 'utf8');
    const opened = await vscode.window.showInformationMessage(
      `expiry-radar: exported to ${path.basename(target.fsPath)}.`,
      'Open',
    );
    if (opened === 'Open') await vscode.env.openExternal(target);
  }

  /**
   * One host, right now, without touching the config. The answer somebody
   * actually wants when they are looking at a hostname in a file and wondering
   * how long its certificate has left.
   */
  async probeHost(): Promise<void> {
    const folder = primaryFolder();
    if (!folder) {
      void vscode.window.showWarningMessage('expiry-radar: open a folder first.');
      return;
    }
    const host = await promptHost();
    if (!host?.trim()) return;

    const s = settingsFor(folder);
    const target = host.trim();
    // Both sources, because "when does this expire" about a hostname means the
    // certificate *and* the registration, and only one of them is usually the
    // one about to bite.
    const domain = target.replace(/:\d+$/, '');
    const result = await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: `expiry-radar: probing ${target}`, cancellable: true },
      async (_progress, token) => {
        try {
          return await collect(
            { folder, endpoints: [target], domains: [domain], ignoreConfig: true, reason: `probe ${target}` },
            s,
            token,
          );
        } catch (err) {
          this.collector.reportFailure(err, { folder, reason: `probe ${target}`, manual: true });
          return undefined;
        }
      },
    );
    if (!result) return;

    const items = toItems(result.report, s.warnWithinDays, s.infoWithinDays);
    if (items.length === 0) {
      const detail = result.result.warnings.join('; ');
      void vscode.window.showWarningMessage(
        `expiry-radar: nothing came back for ${target}${detail ? ` — ${detail}` : ''}.`,
      );
      return;
    }
    // A quick pick rather than a notification: a probe returns the leaf, every
    // intermediate in the chain and the registration, and the intermediate is
    // the one nobody tracks.
    await vscode.window.showQuickPick(
      items.map((item) => ({
        label: `${item.display} — ${humanDays(item.daysLeft)}`,
        description: item.source,
        detail: describe(item).join(' · '),
      })),
      { title: `expiry-radar: ${target}`, placeHolder: `${items.length} item(s), ranked by blast radius` },
    );
  }

  /**
   * Record something. The panel is where you are standing when you notice
   * something is missing, so this writes the config for you rather than leaving
   * you in a JSON editor.
   */
  async addItem(): Promise<void> {
    const folder = primaryFolder();
    if (!folder) {
      void vscode.window.showWarningMessage('expiry-radar: open a folder first.');
      return;
    }
    const kind = await pickEntryKind();
    if (!kind) return;
    const rendered = await promptEntry(kind);
    if (!rendered) return;

    const s = settingsFor(folder);
    const target = resolveConfig(folder, s) || path.join(folder.uri.fsPath, s.configPath || 'expiry-radar.json');
    let existing = '';
    try {
      existing = await fs.promises.readFile(target, 'utf8');
    } catch {
      // No config yet: addToArray writes one around the entry.
    }
    const { text, line } = addToArray(existing, ARRAY_FOR[kind], rendered);
    await fs.promises.writeFile(target, text, 'utf8');

    // Shown, not just written: the entry is now the operator's to check, and a
    // config edited invisibly is one nobody trusts.
    const doc = await vscode.workspace.openTextDocument(target);
    const editor = await vscode.window.showTextDocument(doc);
    const at = new vscode.Range(line - 1, 0, line - 1, 0);
    editor.selection = new vscode.Selection(at.start, at.start);
    editor.revealRange(at);

    // Straight into a collection, so the row appears in the panel rather than
    // waiting for the next refresh to prove the edit worked.
    await this.run('item added');
  }

  async openConfig(): Promise<void> {
    const folder = primaryFolder();
    if (!folder) return;
    const s = settingsFor(folder);
    const existing = resolveConfig(folder, s);
    if (existing) {
      await vscode.window.showTextDocument(await vscode.workspace.openTextDocument(existing));
      return;
    }
    const target = path.join(folder.uri.fsPath, s.configPath || 'expiry-radar.json');
    const example = path.join(folder.uri.fsPath, 'expiry-radar.example.json');
    const choice = await vscode.window.showInformationMessage(
      `expiry-radar: no config at ${path.basename(target)}.`,
      'Create it',
      'Open settings',
    );
    if (choice === 'Open settings') {
      await vscode.commands.executeCommand('workbench.action.openSettings', 'expiryRadar');
      return;
    }
    if (choice !== 'Create it') return;
    // Seeded from the repository's own example when there is one, so a new file
    // shows every source rather than the two that need no credentials.
    const seed = fs.existsSync(example)
      ? await fs.promises.readFile(example, 'utf8')
      : `${JSON.stringify({ endpoints: [{ host: 'shop.example.com' }], domains: ['example.com'] }, null, 2)}\n`;
    await fs.promises.writeFile(target, seed, { encoding: 'utf8', flag: 'wx' });
    await vscode.window.showTextDocument(await vscode.workspace.openTextDocument(target));
  }

  /**
   * Stop tracking a recorded item.
   *
   * Only offered on rows the config recorded. A discovered item has no entry to
   * delete — removing a line would not remove a certificate from an Ingress —
   * and offering it would imply this tool writes to your estate, which it never
   * does.
   */
  async removeItem(node?: Node): Promise<void> {
    if (!node || node.kind !== 'item' || !node.item.origin) return;
    const item = node.item;
    const origin = item.origin!;

    const confirmed = await vscode.window.showWarningMessage(
      `Stop tracking ${item.display}?`,
      { modal: true, detail: `Removes its entry from ${path.basename(origin.file)}. Nothing in your estate is touched.` },
      'Remove',
    );
    if (confirmed !== 'Remove') return;

    let text: string;
    try {
      text = await fs.promises.readFile(origin.file, 'utf8');
    } catch (err) {
      void vscode.window.showErrorMessage(`expiry-radar: could not read the config: ${String(err)}`);
      return;
    }
    // The line came from the last collection; the file may have been edited
    // since. Removing whatever now sits on that line would delete the wrong
    // entry, so check it still names this item before touching anything.
    const onLine = text.split('\n')[origin.line - 1] ?? '';
    if (!onLine.includes(item.name)) {
      void vscode.window.showWarningMessage(
        `expiry-radar: ${path.basename(origin.file)} has changed since the last collection — refresh and try again.`,
      );
      return;
    }
    const key = arrayForSource(item.source);
    const next = key ? removeEntry(text, key, origin.line, origin.column) : undefined;
    if (next === undefined) {
      void vscode.window.showWarningMessage(
        `expiry-radar: could not find the entry for ${item.display} to remove.`,
      );
      return;
    }
    await fs.promises.writeFile(origin.file, next, 'utf8');
    await this.run('item removed');
  }

  async copyItem(node?: Node): Promise<void> {
    if (!node || node.kind !== 'item') return;
    const item = node.item;
    await vscode.env.clipboard.writeText(
      [`${item.display} — ${humanDays(item.daysLeft)}`, item.why, ...describe(item)].join('\n'),
    );
  }

  private async checkEnvironment(): Promise<void> {
    const folder = primaryFolder();
    if (!folder) return;
    await runDoctor(folder, settingsFor(folder));
  }
}
