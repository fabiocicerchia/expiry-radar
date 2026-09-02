/**
 * Activation: build the pieces, wire them to the editor, and register the
 * commands. Everything a command *does* is in `commands.ts`, everything a
 * collection does is in `collector.ts`; what is left here is the wiring, which
 * is the only part that has to be read in order.
 */
import * as vscode from 'vscode';

import { Collector } from './collector';
import { Commands } from './commands';
import { DiagnosticPublisher } from './diagnostics';
import { primaryFolder, settingsFor } from './folder';
import { InventoryView } from './inventoryView';
import { disposeLog, log } from './log';
import { ReportView } from './report';
import { hasSources, resetBinaryCache, resolveConfig } from './runner';
import { Job, Scheduler } from './scheduler';
import { StatusBar } from './status';
import { ResultStore } from './store';

/** Let the window settle before dialling anything. */
const STARTUP_DELAY_MS = 5000;

export function activate(context: vscode.ExtensionContext): void {
  const store = new ResultStore();
  const diagnostics = new DiagnosticPublisher();
  const statusBar = new StatusBar();
  const reportView = new ReportView();
  const inventoryView = new InventoryView(store);
  const collector = new Collector(store, diagnostics, statusBar, inventoryView);

  const scheduler = new Scheduler(
    (job, token) => collector.execute(job, token),
    () => {
      const s = settingsFor(primaryFolder());
      return { debounceMs: s.debounceMs, intervalMinutes: s.intervalMinutes };
    },
  );
  const commands = new Commands(collector, scheduler, reportView, inventoryView, store);
  const armSweep = () => setSweep(scheduler);

  context.subscriptions.push(
    inventoryView.register(),
    inventoryView,
    store.onDidChange(() => collector.paint()),
    store,
    diagnostics,
    statusBar,
    scheduler,
    { dispose: () => reportView.dispose() },
    { dispose: disposeLog },

    ...commands.register(),

    vscode.window.onDidChangeActiveTextEditor(() => inventoryView.refresh()),
    vscode.workspace.onDidSaveTextDocument((doc) => onSave(doc, scheduler)),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration('expiryRadar')) return;
      resetBinaryCache();
      collector.forgetNotFound();
      armSweep();
      collector.publishDiagnostics();
      collector.paint();
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(armSweep),
  );

  armSweep();
  collector.paint();
  scheduleStartupCollection(context, scheduler);

  log().info('expiry-radar extension activated');
}

export function deactivate(): void {
  // Everything is registered in context.subscriptions.
}

/** (Re)arm the periodic refresh from whatever the settings now say. */
function setSweep(scheduler: Scheduler): void {
  const s = settingsFor(primaryFolder());
  scheduler.setSweep(s.trigger === 'interval' || s.trigger === 'onConfigSaveAndInterval', () => {
    const folder = primaryFolder();
    if (!folder || !hasSources(folder, settingsFor(folder))) return undefined;
    return { folder, reason: 'periodic refresh', manual: false };
  });
}

/**
 * Automatic runs stay quiet in a folder with nothing configured. A command
 * still explains itself — see `Commands.run` — but a popup five seconds after
 * every window opens, in a repository that has nothing to do with this tool,
 * would be the reason somebody disables the extension.
 */
function scheduleAutomatic(job: Job, scheduler: Scheduler): void {
  if (!hasSources(job.folder, settingsFor(job.folder))) {
    log().debug(`no sources configured in ${job.folder.name} — not collecting`);
    return;
  }
  scheduler.schedule(job);
}

function onSave(doc: vscode.TextDocument, scheduler: Scheduler): void {
  if (doc.uri.scheme !== 'file') return;
  const folder = vscode.workspace.getWorkspaceFolder(doc.uri);
  if (!folder) return;
  const s = settingsFor(folder);
  if (s.trigger !== 'onConfigSave' && s.trigger !== 'onConfigSaveAndInterval') return;
  // Only the config file. Every other save in the repository has nothing to do
  // with what the estate has expiring, and re-dialling every host because
  // somebody saved a README would be indefensible.
  if (doc.uri.fsPath !== resolveConfig(folder, s)) return;
  scheduleAutomatic({ folder, reason: 'config saved', manual: false }, scheduler);
}

function scheduleStartupCollection(
  context: vscode.ExtensionContext,
  scheduler: Scheduler,
): void {
  const startup = settingsFor(primaryFolder());
  if (!startup.scanOnStartup || startup.trigger === 'manual') return;
  const timer = setTimeout(() => {
    const folder = primaryFolder();
    if (folder) scheduleAutomatic({ folder, reason: 'startup', manual: false }, scheduler);
  }, STARTUP_DELAY_MS);
  context.subscriptions.push({ dispose: () => clearTimeout(timer) });
}
