import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';

import { readSettings, Settings } from './config';
import { DiagnosticGroup, DiagnosticPublisher } from './diagnostics';
import { runDoctor } from './doctor';
import { addToArray, ARRAY_FOR, EntryKind, invalidExpires, MANUAL_KINDS, renderEntry } from './edit';
import { InventoryView, Node } from './inventoryView';
import { declaredIn } from './locate';
import { disposeLog, log } from './log';
import { describe, humanDays, toItems } from './parse';
import { ReportView } from './report';
import {
  collect,
  Format,
  hasSources,
  promptInstall,
  RadarNotFoundError,
  resetBinaryCache,
  resolveConfig,
  runRadar,
} from './runner';
import { Job, Scheduler } from './scheduler';
import { StatusBar } from './status';
import { ResultStore } from './store';
import { Item, Snapshot } from './types';

const ERROR_COOLDOWN_MS = 60_000;

/**
 * The sources whose items were declared in the config file, and so have a line
 * to point at. Everything else was discovered — a certificate on an Ingress was
 * never written down here, and squiggling a config line for it would be a lie.
 */
const DECLARED_BY = new Set(['tls:endpoint', 'domain:rdap', 'domain:whois']);

export function activate(context: vscode.ExtensionContext): void {
  const store = new ResultStore();
  const diagnostics = new DiagnosticPublisher();
  const statusBar = new StatusBar();
  const reportView = new ReportView();
  const inventoryView = new InventoryView(store);

  let lastErrorAt = 0;
  let notFoundShown = false;
  /** Label of the collection in flight, if any — the status bar belongs to it. */
  let collecting: string | undefined;

  const settingsFor = (folder?: vscode.WorkspaceFolder): Settings => readSettings(folder?.uri);

  const primaryFolder = (): vscode.WorkspaceFolder | undefined => {
    const active = vscode.window.activeTextEditor?.document.uri;
    return (
      (active ? vscode.workspace.getWorkspaceFolder(active) : undefined) ??
      vscode.workspace.workspaceFolders?.[0]
    );
  };

  const paint = () => {
    const folder = primaryFolder();
    inventoryView.refresh();
    if (collecting !== undefined) return; // The run owns the status bar.
    const s = settingsFor(folder);
    statusBar.idle(folder ? store.get(folder) : undefined, s.statusWarnWithinDays);
  };

  const publishDiagnostics = () => {
    const groups: DiagnosticGroup[] = store
      .folders()
      .map((f) => ({ items: store.items(f), settings: settingsFor(f) }));
    diagnostics.publish(groups);
  };

  /**
   * Attach each item to the line that asked for it. Read fresh per run rather
   * than cached: the config is a file somebody edits, and a stale map would put
   * a squiggle on the wrong line — worse than none at all.
   */
  const withOrigins = async (items: Item[], configPath: string): Promise<Item[]> => {
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
  };

  const reportFailure = (err: unknown, job: Job): void => {
    if (err instanceof vscode.CancellationError) {
      log().info(`collection cancelled: ${job.reason}`);
      return;
    }
    if (err instanceof RadarNotFoundError) {
      log().error(err.message);
      if (notFoundShown) return;
      notFoundShown = true;
      void promptInstall(err.message);
      return;
    }
    const message = err instanceof Error ? err.message : String(err);
    log().error(message);
    // Automatic runs fail quietly after the first notification — a broken
    // binary must not produce a popup every hour.
    if (job.manual || Date.now() - lastErrorAt > ERROR_COOLDOWN_MS) {
      lastErrorAt = Date.now();
      void vscode.window
        .showErrorMessage(`expiry-radar: ${message}`, 'Show log')
        .then((choice) => {
          if (choice === 'Show log') log().show(true);
        });
    }
  };

  const execute = async (job: Job, token: vscode.CancellationToken): Promise<void> => {
    const s = settingsFor(job.folder);
    collecting = job.folder.name;
    inventoryView.setCollecting(job.folder.name);
    statusBar.collecting(job.folder.name);
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
      store.set(job.folder, snapshot);
      publishDiagnostics();
      if (result.warnings.length) announceWarnings(result.warnings, job);
    } catch (err) {
      reportFailure(err, job);
    } finally {
      collecting = undefined;
      inventoryView.setCollecting('');
      paint();
    }
  };

  /**
   * A source that failed is not a log line. The whole product is a complete
   * inventory ranked by consequence, and a run that lost a source produces a
   * report that looks exactly like a clean estate.
   */
  let lastWarningAt = 0;
  const announceWarnings = (warnings: string[], job: Job): void => {
    if (!job.manual && Date.now() - lastWarningAt < ERROR_COOLDOWN_MS) return;
    lastWarningAt = Date.now();
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
  };

  /**
   * Automatic runs stay quiet in a folder with nothing configured. A command
   * still explains itself — see `run` — but a popup five seconds after every
   * window opens, in a repository that has nothing to do with this tool, would
   * be the reason somebody disables the extension.
   */
  const scheduleAutomatic = (job: Job): void => {
    const folder = job.folder;
    if (!hasSources(folder, settingsFor(folder))) {
      log().debug(`no sources configured in ${folder.name} — not collecting`);
      return;
    }
    scheduler.schedule(job);
  };

  const scheduler = new Scheduler(execute, () => {
    const s = settingsFor(primaryFolder());
    return { debounceMs: s.debounceMs, intervalMinutes: s.intervalMinutes };
  });

  const armSweep = () => {
    const s = settingsFor(primaryFolder());
    scheduler.setSweep(s.trigger === 'interval' || s.trigger === 'onConfigSaveAndInterval', () => {
      const folder = primaryFolder();
      if (!folder || !hasSources(folder, settingsFor(folder))) return undefined;
      return { folder, reason: 'periodic refresh', manual: false };
    });
  };

  /** A manual collection, with a cancellable progress notification. */
  const run = async (reason: string): Promise<boolean> => {
    const folder = primaryFolder();
    if (!folder) {
      void vscode.window.showWarningMessage('expiry-radar: open a folder first.');
      return false;
    }
    const settings = settingsFor(folder);
    if (!hasSources(folder, settings)) {
      // The CLI would exit 2 saying exactly this. Say it here with the thing
      // that fixes it attached, rather than as a stack of shell output.
      const choice = await vscode.window.showWarningMessage(
        'expiry-radar: no sources configured — every source is opt-in, so there is nothing to collect.',
        'Open config file',
        'Open settings',
      );
      if (choice === 'Open config file') await openConfig();
      else if (choice === 'Open settings') {
        await vscode.commands.executeCommand('workbench.action.openSettings', 'expiryRadar');
      }
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
        token.onCancellationRequested(() => scheduler.cancelJob(job));
        return scheduler.runNow(job);
      },
    );
    return store.get(folder) !== undefined;
  };

  /**
   * One `-format <fmt>` run, outside the scheduler's single-flight lane on
   * purpose: it produces a document, not the board, and making the report wait
   * behind a periodic refresh would be a spinner for no reason.
   */
  const render = async (format: Format, title: string): Promise<string | undefined> => {
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
          if (result.warnings.length) announceWarnings(result.warnings, { folder, reason: title, manual: true });
          return result.stdout;
        } catch (err) {
          reportFailure(err, { folder, reason: title, manual: true });
          return undefined;
        }
      },
    );
  };

  /** When the report in the tab was rendered, so a stale one is replaced. */
  let reportRenderedAt = 0;

  const openReport = async (force: boolean): Promise<void> => {
    const folder = primaryFolder();
    if (!folder) return;
    // A report older than the newest collection would show a different estate
    // from the panel next to it.
    const stale = (store.get(folder)?.at ?? 0) > reportRenderedAt;
    if (force || stale || !reportView.current) {
      // The CLI renders one format per invocation, so the report is its own
      // collection — this dials the estate a second time, which is why it is
      // only ever done on demand and never on a background refresh.
      const html = await render('html', 'expiry-radar: rendering the report');
      if (!html) return;
      reportView.current = html;
      reportRenderedAt = Date.now();
    }
    reportView.show(reportView.current, folder.name);
  };

  const exportReport = async (): Promise<void> => {
    const folder = primaryFolder();
    if (!folder) return;
    const choice = await vscode.window.showQuickPick(
      [
        { label: 'HTML report', description: 'self-contained, for mailing or publishing', format: 'html' as Format, ext: 'html' },
        { label: 'iCal feed', description: 'renewals as calendar events, with alarms by blast radius', format: 'ical' as Format, ext: 'ics' },
        { label: 'JSON', description: 'the ranked inventory, for CI', format: 'json' as Format, ext: 'json' },
        { label: 'Prometheus metrics', description: 'a scrape body', format: 'prometheus' as Format, ext: 'prom' },
      ],
      { title: 'expiry-radar: export as', placeHolder: 'Pick a format' },
    );
    if (!choice) return;

    const body = await render(choice.format, `expiry-radar: rendering ${choice.label}`);
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
  };

  /**
   * One host, right now, without touching the config. The answer somebody
   * actually wants when they are looking at a hostname in a file and wondering
   * how long its certificate has left.
   */
  const probeHost = async (): Promise<void> => {
    const folder = primaryFolder();
    if (!folder) {
      void vscode.window.showWarningMessage('expiry-radar: open a folder first.');
      return;
    }
    const editor = vscode.window.activeTextEditor;
    const selected = editor && !editor.selection.isEmpty ? editor.document.getText(editor.selection).trim() : '';
    const host = await vscode.window.showInputBox({
      title: 'expiry-radar: probe a host',
      prompt: 'Host to probe over TLS, and check as a domain. Port optional.',
      value: selected,
      placeHolder: 'shop.example.com',
      validateInput: (value) => (value.trim() ? undefined : 'A host is required.'),
    });
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
          reportFailure(err, { folder, reason: `probe ${target}`, manual: true });
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
  };

  /**
   * Record something. Two of the six kinds of item are recorded rather than
   * discovered — a host to probe and a domain to look up — and the third option
   * is for what nothing can find at all: a registrar with no RDAP, a credential
   * rotated by hand, a code-signing certificate on somebody's laptop.
   *
   * The panel is where you are standing when you notice something is missing,
   * so this writes the config for you rather than leaving you in a JSON editor.
   */
  const addItem = async (): Promise<void> => {
    const folder = primaryFolder();
    if (!folder) {
      void vscode.window.showWarningMessage('expiry-radar: open a folder first.');
      return;
    }
    const what = await vscode.window.showQuickPick(
      [
        {
          label: 'Endpoint',
          detail: 'A host to probe over TLS — its certificate and every intermediate in its chain.',
          entry: 'endpoint' as EntryKind,
        },
        {
          label: 'Domain',
          detail: 'A registration to check via RDAP.',
          entry: 'domain' as EntryKind,
        },
        {
          label: 'Something nothing can discover',
          detail:
            'A date you know: a registrar with no RDAP, a credential rotated by hand, a contract.',
          entry: 'manual' as EntryKind,
        },
      ],
      { title: 'expiry-radar: record what?', placeHolder: 'Everything else is discovered, not recorded' },
    );
    if (!what) return;

    const rendered = await promptEntry(what.entry);
    if (!rendered) return;

    const s = settingsFor(folder);
    const target = resolveConfig(folder, s) || path.join(folder.uri.fsPath, s.configPath || 'expiry-radar.json');
    let existing = '';
    try {
      existing = await fs.promises.readFile(target, 'utf8');
    } catch {
      // No config yet: addToArray writes one around the entry.
    }
    const { text, line } = addToArray(existing, ARRAY_FOR[what.entry], rendered);
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
    await run('item added');
  };

  /** The prompts for one kind of entry, or undefined if the user backed out. */
  const promptEntry = async (kind: EntryKind): Promise<string | undefined> => {
    if (kind !== 'manual') {
      const isHost = kind === 'endpoint';
      const value = await vscode.window.showInputBox({
        title: isHost ? 'expiry-radar: record an endpoint' : 'expiry-radar: record a domain',
        prompt: isHost ? 'Host to probe over TLS. Port optional.' : 'Domain to check via RDAP.',
        placeHolder: isHost ? 'shop.example.com' : 'example.com',
        value: selectedText(),
        validateInput: (v) => (v.trim() ? undefined : 'A value is required.'),
      });
      return value?.trim() ? renderEntry(kind, value) : undefined;
    }

    const name = await vscode.window.showInputBox({
      title: 'expiry-radar: record an item — 1 of 3',
      prompt: 'What is it? This is the name the report will show.',
      placeHolder: 'acme-corp.co.uk',
      value: selectedText(),
      validateInput: (v) => (v.trim() ? undefined : 'A name is required.'),
    });
    if (!name?.trim()) return undefined;

    // The kind is not cosmetic: it picks the base blast radius, which is what
    // decides where this lands in the ranking.
    const kindPick = await vscode.window.showQuickPick(
      MANUAL_KINDS.map((k) => ({ label: k.label, detail: k.hint, itemKind: k.kind })),
      {
        title: 'expiry-radar: record an item — 2 of 3',
        placeHolder: 'What kind? This sets its base blast radius.',
      },
    );
    if (!kindPick) return undefined;

    const expires = await vscode.window.showInputBox({
      title: 'expiry-radar: record an item — 3 of 3',
      prompt: 'When does it expire? YYYY-MM-DD, or a full RFC 3339 timestamp.',
      placeHolder: '2027-03-01',
      validateInput: invalidExpires,
    });
    if (!expires?.trim()) return undefined;

    return renderEntry('manual', { name, kind: kindPick.itemKind, expires });
  };

  /** A selection is usually the thing being recorded — offer it as the default. */
  const selectedText = (): string => {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.selection.isEmpty) return '';
    return editor.document.getText(editor.selection).trim();
  };

  const openConfig = async (): Promise<void> => {
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
  };

  const copyItem = async (node?: Node): Promise<void> => {
    if (!node || node.kind !== 'item') return;
    const item = node.item;
    await vscode.env.clipboard.writeText(
      [`${item.display} — ${humanDays(item.daysLeft)}`, item.why, ...describe(item)].join('\n'),
    );
  };

  const onSave = (doc: vscode.TextDocument): void => {
    if (doc.uri.scheme !== 'file') return;
    const folder = vscode.workspace.getWorkspaceFolder(doc.uri);
    if (!folder) return;
    const s = settingsFor(folder);
    if (s.trigger !== 'onConfigSave' && s.trigger !== 'onConfigSaveAndInterval') return;
    // Only the config file. Every other save in the repository has nothing to
    // do with what the estate has expiring, and re-dialling every host because
    // somebody saved a README would be indefensible.
    if (doc.uri.fsPath !== resolveConfig(folder, s)) return;
    scheduleAutomatic({ folder, reason: 'config saved', manual: false });
  };

  context.subscriptions.push(
    inventoryView.register(),
    inventoryView,
    store.onDidChange(paint),
    store,
    diagnostics,
    statusBar,
    scheduler,
    { dispose: () => reportView.dispose() },
    { dispose: disposeLog },

    vscode.commands.registerCommand('expiryRadar.scan', () => run('command')),
    vscode.commands.registerCommand('expiryRadar.showReport', () => openReport(false)),
    vscode.commands.registerCommand('expiryRadar.exportReport', () => exportReport()),
    vscode.commands.registerCommand('expiryRadar.probeHost', () => probeHost()),
    vscode.commands.registerCommand('expiryRadar.addItem', () => addItem()),
    vscode.commands.registerCommand('expiryRadar.openConfig', () => openConfig()),
    vscode.commands.registerCommand('expiryRadar.cancel', () => scheduler.cancel()),
    vscode.commands.registerCommand('expiryRadar.showLog', () => log().show(true)),
    vscode.commands.registerCommand('expiryRadar.filterItems', () => inventoryView.pickFilters()),
    vscode.commands.registerCommand('expiryRadar.groupByKind', () => inventoryView.setGrouping('kind')),
    vscode.commands.registerCommand('expiryRadar.groupByRank', () => inventoryView.setGrouping('rank')),
    vscode.commands.registerCommand('expiryRadar.expandAll', () => inventoryView.expandAll()),
    vscode.commands.registerCommand('expiryRadar.copyItem', (node?: Node) => copyItem(node)),
    vscode.commands.registerCommand('expiryRadar.checkEnvironment', async () => {
      const folder = primaryFolder();
      if (folder) await runDoctor(folder, settingsFor(folder));
    }),

    vscode.window.onDidChangeActiveTextEditor(() => inventoryView.refresh()),
    vscode.workspace.onDidSaveTextDocument(onSave),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration('expiryRadar')) return;
      resetBinaryCache();
      notFoundShown = false;
      armSweep();
      publishDiagnostics();
      paint();
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(armSweep),
  );

  armSweep();
  paint();

  const startup = settingsFor(primaryFolder());
  if (startup.scanOnStartup && startup.trigger !== 'manual') {
    // Let the window settle before dialling anything.
    const timer = setTimeout(() => {
      const folder = primaryFolder();
      if (folder) scheduleAutomatic({ folder, reason: 'startup', manual: false });
    }, 5000);
    context.subscriptions.push({ dispose: () => clearTimeout(timer) });
  }

  log().info('expiry-radar extension activated');
}

export function deactivate(): void {
  // Everything is registered in context.subscriptions.
}
