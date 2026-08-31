import * as vscode from 'vscode';

import { humanDays, kindLabel } from './parse';
import { Snapshot } from './types';

/**
 * The status bar answers one question: what breaks next, and when.
 *
 * The soonest deadline rather than the highest priority. Priority is the right
 * order for a list you are working through; a single glyph in the corner is a
 * clock, and a clock that showed the second-soonest deadline would be wrong in
 * exactly the case it matters.
 */
export class StatusBar {
  private readonly item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);

  constructor() {
    this.item.command = 'expiryRadar.showReport';
    this.item.name = 'expiry-radar';
  }

  collecting(label: string): void {
    this.item.text = '$(sync~spin) expiry-radar';
    this.item.tooltip = `Collecting ${label}…`;
    this.item.backgroundColor = undefined;
    this.item.show();
  }

  idle(snapshot: Snapshot | undefined, warnWithinDays: number): void {
    if (!snapshot) {
      this.item.text = '$(radio-tower) expiry-radar';
      this.item.tooltip =
        'Nothing collected yet — click to open the report, or run "expiry-radar: Refresh Inventory".';
      this.item.backgroundColor = undefined;
      this.item.show();
      return;
    }

    const soonest = snapshot.items.reduce<(typeof snapshot.items)[number] | undefined>(
      (worst, item) => (!worst || item.daysLeft < worst.daysLeft ? item : worst),
      undefined,
    );
    const expired = snapshot.items.filter((i) => i.daysLeft < 0).length;

    if (!soonest) {
      this.item.text = '$(radio-tower) expiry-radar 0';
      this.item.tooltip = new vscode.MarkdownString(
        this.lines(snapshot, ['**Nothing expiring** in the configured sources.']).join('\n'),
      );
      this.item.backgroundColor = warningBackground(snapshot);
      this.item.show();
      return;
    }

    const icon = expired > 0 ? 'error' : soonest.daysLeft <= warnWithinDays ? 'warning' : 'watch';
    this.item.text = `$(${icon}) expiry-radar ${humanDays(soonest.daysLeft)}`;
    this.item.tooltip = new vscode.MarkdownString(
      this.lines(snapshot, [
        `**Next: ${soonest.display}** — ${humanDays(soonest.daysLeft)}`,
        '',
        `${kindLabel(soonest.kind)} from \`${soonest.source}\``,
        `Blast radius ${soonest.blastRadius.toFixed(2)}: ${soonest.why}`,
        '',
        `Tracking ${snapshot.items.length} item(s)${expired ? `, ${expired} already expired` : ''}.`,
      ]).join('\n'),
    );
    this.item.backgroundColor =
      expired > 0
        ? new vscode.ThemeColor('statusBarItem.errorBackground')
        : soonest.daysLeft <= warnWithinDays
          ? new vscode.ThemeColor('statusBarItem.warningBackground')
          : warningBackground(snapshot);
    this.item.show();
  }

  /**
   * A failed source is on the tooltip whatever else it says. An inventory
   * missing a source reads exactly like a clean estate, and the status bar is
   * the one part of this that is always on screen.
   */
  private lines(snapshot: Snapshot, body: string[]): string[] {
    const out = [...body];
    if (snapshot.warnings.length) {
      out.push('', `⚠ ${snapshot.warnings.length} source(s) failed — this inventory is incomplete:`);
      for (const warning of snapshot.warnings) out.push(`- ${warning}`);
    }
    out.push('', 'Click to open the report.');
    return out;
  }

  dispose(): void {
    this.item.dispose();
  }
}

function warningBackground(snapshot: Snapshot): vscode.ThemeColor | undefined {
  return snapshot.warnings.length
    ? new vscode.ThemeColor('statusBarItem.warningBackground')
    : undefined;
}
