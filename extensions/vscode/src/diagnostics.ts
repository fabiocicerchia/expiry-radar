/**
 * Expiring items -> squiggles on the config file.
 *
 * Only the two sources that are declared in the repository get one: the host in
 * `endpoints` and the string in `domains`. Everything else — a certificate on an
 * Ingress, a key in IAM, a lease in Vault — was never written down here, and
 * there is no honest line to attach it to. Those live in the panel.
 *
 * The message carries the whole explanation, including *why* the blast radius
 * came out where it did: a ranking nobody can explain gets ignored, and a hover
 * is where somebody actually reads it.
 */
import * as vscode from 'vscode';

import { Settings } from './config';
import { describe, humanDays } from './parse';
import { Item } from './types';

const SEVERITY: Record<string, vscode.DiagnosticSeverity> = {
  expired: vscode.DiagnosticSeverity.Error,
  urgent: vscode.DiagnosticSeverity.Warning,
  soon: vscode.DiagnosticSeverity.Information,
};

export function message(item: Item): string {
  const head =
    item.daysLeft < 0
      ? `${item.display} expired ${Math.floor(-item.daysLeft)} day(s) ago`
      : `${item.display} expires in ${humanDays(item.daysLeft)}`;
  return `${head}.\n\n${item.why}\n\n${describe(item).join(' · ')}`;
}

/** One folder's items, with the settings that folder resolves to. */
export interface DiagnosticGroup {
  items: Item[];
  settings: Settings;
}

export class DiagnosticPublisher {
  private readonly collection = vscode.languages.createDiagnosticCollection('expiry-radar');

  /**
   * Republishes everything at once. A partial publish is not an option: the
   * collection is global, so writing only the folder that was just collected
   * would drop the other folders' diagnostics in a multi-root workspace.
   */
  publish(groups: DiagnosticGroup[]): void {
    const byFile = new Map<string, vscode.Diagnostic[]>();

    for (const { items, settings } of groups) {
      if (!settings.diagnosticsEnabled) continue;
      for (const item of items) {
        if (!item.origin) continue; // Nothing declared it here; the panel has it.
        const severity = severityFor(item, settings);
        if (severity === undefined) continue;

        const line = Math.max(0, item.origin.line - 1);
        const column = Math.max(0, item.origin.column - 1);
        const diagnostic = new vscode.Diagnostic(
          // End-of-line is clamped by the editor, so the whole entry is covered
          // without opening the document to measure it.
          new vscode.Range(line, column, line, Number.MAX_SAFE_INTEGER),
          message(item),
          severity,
        );
        diagnostic.source = 'expiry-radar';
        diagnostic.code = item.kind;
        const list = byFile.get(item.origin.file);
        if (list) list.push(diagnostic);
        else byFile.set(item.origin.file, [diagnostic]);
      }
    }

    // One call, not one per file: each `set` crosses to the renderer.
    const entries: [vscode.Uri, vscode.Diagnostic[]][] = [];
    for (const [file, diagnostics] of byFile) entries.push([vscode.Uri.file(file), diagnostics]);
    this.collection.set(entries);
  }

  clear(): void {
    this.collection.clear();
  }

  dispose(): void {
    this.collection.dispose();
  }
}

/**
 * Deadline, not priority. A cert on the payment path 80 days out is the top of
 * the panel and still nothing to interrupt an edit over; the cert that expires
 * on Friday is what a squiggle is for.
 */
function severityFor(item: Item, s: Settings): vscode.DiagnosticSeverity | undefined {
  if (item.daysLeft < 0) return SEVERITY.expired;
  if (item.daysLeft <= s.warnWithinDays) return SEVERITY.urgent;
  if (item.daysLeft <= s.infoWithinDays) return SEVERITY.soon;
  return undefined;
}
