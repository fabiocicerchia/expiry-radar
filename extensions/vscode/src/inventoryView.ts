/**
 * The bottom pane: a plain VS Code tree view.
 *
 * Everything here is native — tree items with per-severity icons, the view
 * title bar for the filters, the view badge for what has already expired, the
 * view message for the "these sources failed" notice. That buys type-ahead
 * filtering, keyboard navigation, theming and accessibility for free, and keeps
 * the pane looking like the Problems panel next to it.
 *
 * The default shape is one ranked list, because the ranking is the product. The
 * kind grouping is there for the other question — "what certificates do we
 * have" — and is deliberately not the default.
 */
import * as vscode from 'vscode';

import {
  compareItems,
  humanDays,
  KINDS,
  kindLabel,
  SEVERITIES,
  SEVERITY_LABEL,
  SEVERITY_RANK,
} from './parse';
import { ResultStore } from './store';
import { Item, Kind, Severity } from './types';

type Grouping = 'rank' | 'kind';

interface GroupNode {
  kind: 'group';
  id: string;
  label: string;
  children: ItemNode[];
}

interface ItemNode {
  kind: 'item';
  id: string;
  item: Item;
}

export type Node = GroupNode | ItemNode;

const ICONS: Record<Severity, { id: string; color: string }> = {
  expired: { id: 'error', color: 'charts.red' },
  urgent: { id: 'warning', color: 'problemsWarningIcon.foreground' },
  soon: { id: 'clock', color: 'charts.yellow' },
  ok: { id: 'pass', color: 'charts.green' },
};

export class InventoryView implements vscode.TreeDataProvider<Node> {
  static readonly viewId = 'expiryRadar.inventory';

  private view?: vscode.TreeView<Node>;
  private grouping: Grouping = 'rank';
  private severities = new Set<Severity>(SEVERITIES);
  private kinds = new Set<Kind>();
  private collectLabel = '';
  /**
   * Bumped by Expand All. Tree item ids carry it, so a bump makes every node
   * new to the editor, which then applies our Expanded collapsible state
   * instead of the collapse it had remembered. Stable otherwise, so a group the
   * user collapsed by hand stays collapsed across refreshes.
   */
  private expansion = 0;
  private model?: Node[];
  private allItems?: Item[];
  private visibleItems?: Item[];

  private readonly changed = new vscode.EventEmitter<Node | undefined>();
  readonly onDidChangeTreeData = this.changed.event;

  constructor(private readonly store: ResultStore) {}

  register(): vscode.Disposable {
    this.view = vscode.window.createTreeView(InventoryView.viewId, {
      treeDataProvider: this,
      showCollapseAll: true,
    });
    void vscode.commands.executeCommand('setContext', 'expiryRadar.grouping', this.grouping);
    this.refresh();
    return this.view;
  }

  setGrouping(grouping: Grouping): void {
    this.grouping = grouping;
    void vscode.commands.executeCommand('setContext', 'expiryRadar.grouping', grouping);
    this.refresh();
  }

  expandAll(): void {
    this.expansion += 1;
    this.refresh();
  }

  setCollecting(label: string): void {
    this.collectLabel = label;
    this.refresh();
  }

  refresh(): void {
    this.model = undefined;
    this.allItems = undefined;
    this.visibleItems = undefined;
    this.changed.fire(undefined);
    this.paintChrome();
  }

  /**
   * One native multi-select quick pick over both axes: how close the deadline
   * is, and what the thing is. They are separate questions, and an item has to
   * pass both.
   */
  async pickFilters(): Promise<void> {
    const severityCount = new Map<Severity, number>();
    const kindCount = new Map<Kind, number>();
    for (const item of this.all()) {
      severityCount.set(item.severity, (severityCount.get(item.severity) ?? 0) + 1);
      kindCount.set(item.kind, (kindCount.get(item.kind) ?? 0) + 1);
    }
    // Kinds the estate actually has, in the order rank weights them, plus
    // anything a newer source added that this build has never heard of.
    const kinds = [
      ...KINDS.filter((k) => kindCount.has(k)),
      ...[...kindCount.keys()].filter((k) => !KINDS.includes(k)).sort(),
    ];

    type Entry = vscode.QuickPickItem & { severity?: Severity; kindKey?: Kind };
    const entries: Entry[] = [
      { label: 'Deadline', kind: vscode.QuickPickItemKind.Separator },
      ...SEVERITIES.map((s) => ({
        label: SEVERITY_LABEL[s],
        description: `${severityCount.get(s) ?? 0}`,
        picked: this.severities.has(s),
        severity: s,
      })),
      { label: 'Kind', kind: vscode.QuickPickItemKind.Separator },
      ...kinds.map((k) => ({
        label: kindLabel(k),
        description: `${kindCount.get(k) ?? 0}`,
        picked: this.kinds.size === 0 || this.kinds.has(k),
        kindKey: k,
      })),
    ];

    const chosen = await vscode.window.showQuickPick(entries, {
      canPickMany: true,
      title: 'expiry-radar: show which items',
      placeHolder: 'An item has to match a selected deadline and a selected kind',
    });
    if (!chosen) return;

    const severities = new Set(chosen.filter((c) => c.severity).map((c) => c.severity as Severity));
    const picked = new Set(chosen.filter((c) => c.kindKey).map((c) => c.kindKey as Kind));
    // Clearing a whole axis would empty the pane with no way back from the pane
    // itself, so an empty axis means "all of it".
    this.severities = severities.size ? severities : new Set(SEVERITIES);
    // An empty kind set is "every kind", including kinds a later run introduces
    // — pinning today's list would silently hide tomorrow's source.
    this.kinds = picked.size === kinds.length ? new Set() : picked;
    this.refresh();
  }

  private get filtered(): boolean {
    return this.severities.size < SEVERITIES.length || this.kinds.size > 0;
  }

  // --- data ------------------------------------------------------------------

  private folder(): vscode.WorkspaceFolder | undefined {
    const active = vscode.window.activeTextEditor?.document.uri;
    return (
      (active ? vscode.workspace.getWorkspaceFolder(active) : undefined) ??
      this.store.folders()[0] ??
      vscode.workspace.workspaceFolders?.[0]
    );
  }

  private all(): Item[] {
    if (this.allItems) return this.allItems;
    const folder = this.folder();
    return (this.allItems = folder ? this.store.items(folder) : []);
  }

  private visible(): Item[] {
    if (this.visibleItems) return this.visibleItems;
    const all = this.all();
    if (!this.filtered) return (this.visibleItems = all);
    return (this.visibleItems = all.filter(
      (i) => this.severities.has(i.severity) && (this.kinds.size === 0 || this.kinds.has(i.kind)),
    ));
  }

  /**
   * Built once per refresh. Rebuilding it inside getChildren — which the editor
   * calls once per node — would make a repaint O(groups x items).
   */
  private build(): Node[] {
    const items = [...this.visible()].sort(compareItems);
    const rev = this.expansion;

    if (this.grouping === 'rank') {
      return items.map((item) => ({ kind: 'item', id: `${rev}:${item.id}`, item }));
    }

    const groups = new Map<string, GroupNode>();
    for (const item of items) {
      let group = groups.get(item.kind);
      if (!group) {
        group = { kind: 'group', id: `${rev}:kind:${item.kind}`, label: kindLabel(item.kind), children: [] };
        groups.set(item.kind, group);
      }
      group.children.push({ kind: 'item', id: `${rev}:${group.children.length}:${item.id}`, item });
    }
    // Groups in worst-deadline order, so the kind with something already broken
    // is at the top rather than wherever the alphabet put it.
    return [...groups.values()].sort(
      (a, b) => SEVERITY_RANK[a.children[0].item.severity] - SEVERITY_RANK[b.children[0].item.severity],
    );
  }

  private get tree(): Node[] {
    if (!this.model) this.model = this.build();
    return this.model;
  }

  getChildren(element?: Node): Node[] {
    if (!element) return this.tree;
    return element.kind === 'group' ? element.children : [];
  }

  getTreeItem(node: Node): vscode.TreeItem {
    if (node.kind === 'group') {
      const item = new vscode.TreeItem(node.label, vscode.TreeItemCollapsibleState.Expanded);
      item.id = node.id;
      item.iconPath = new vscode.ThemeIcon('folder');
      item.description = `${node.children.length} · soonest ${humanDays(node.children[0].item.daysLeft)}`;
      item.contextValue = 'expiryRadarGroup';
      return item;
    }

    const entry = node.item;
    const item = new vscode.TreeItem(entry.display, vscode.TreeItemCollapsibleState.None);
    item.id = node.id;
    const icon = ICONS[entry.severity];
    item.iconPath = new vscode.ThemeIcon(icon.id, new vscode.ThemeColor(icon.color));
    item.description = [
      humanDays(entry.daysLeft),
      this.grouping === 'rank' ? kindLabel(entry.kind) : '',
      entry.source,
      `p ${entry.priority.toFixed(2)}`,
    ]
      .filter(Boolean)
      .join(' · ');
    item.tooltip = this.tooltip(entry);
    // Recorded rows can be edited and removed; discovered ones cannot, because
    // deleting a config line would not delete a certificate from an Ingress.
    item.contextValue = entry.origin ? 'expiryRadarRecorded' : 'expiryRadarItem';
    if (entry.origin) {
      const uri = vscode.Uri.file(entry.origin.file);
      const line = Math.max(0, entry.origin.line - 1);
      const column = Math.max(0, entry.origin.column - 1);
      item.resourceUri = uri;
      item.command = {
        command: 'vscode.open',
        title: 'Open the line that declared this',
        arguments: [
          uri,
          {
            selection: new vscode.Range(line, column, line, column),
          } satisfies vscode.TextDocumentShowOptions,
        ],
      };
    }
    return item;
  }

  private tooltip(item: Item): vscode.MarkdownString {
    const md = new vscode.MarkdownString();
    md.appendMarkdown(`**${item.display}** — ${humanDays(item.daysLeft)}\n\n`);
    md.appendMarkdown(`${item.why}\n\n`);
    md.appendMarkdown(
      [
        `**kind** ${kindLabel(item.kind)}`,
        `**source** \`${item.source}\``,
        `**expires** ${item.expires.slice(0, 10)}`,
        `**priority** ${item.priority.toFixed(2)}`,
        `**blast radius** ${item.blastRadius.toFixed(2)}`,
      ].join(' · '),
    );
    const labels = Object.entries(item.labels ?? {});
    if (labels.length) {
      md.appendMarkdown(`\n\n${labels.map(([k, v]) => `\`${k}=${v}\``).join(' ')}`);
    }
    md.appendMarkdown(
      item.origin
        ? '\n\n_Recorded in the config file — click to open the line, or right-click to remove._'
        : '\n\n_Discovered by a source, not recorded in the config file._',
    );
    return md;
  }

  // --- title bar / message / badge -------------------------------------------

  private paintChrome(): void {
    const view = this.view;
    if (!view) return;
    const folder = this.folder();
    const snapshot = folder ? this.store.get(folder) : undefined;
    const visible = this.visible();

    const collectedAt = snapshot
      ? `${snapshot.items.length} item(s) · ${new Date(snapshot.at).toLocaleTimeString()}`
      : undefined;
    view.description = this.collectLabel
      ? `collecting ${this.collectLabel}…`
      : [collectedAt, this.filtered ? 'filtered' : ''].filter(Boolean).join(' · ') || undefined;

    // The badge counts what is already broken plus what breaks next — the rows
    // somebody has to do something about today.
    const urgent = visible.filter((i) => i.severity === 'expired' || i.severity === 'urgent').length;
    view.badge = urgent ? { value: urgent, tooltip: `${urgent} expired or expiring soon` } : undefined;

    view.message = this.message(visible.length, snapshot);
  }

  private message(shown: number, snapshot: ReturnType<ResultStore['get']>): string | undefined {
    const lines: string[] = [];

    if (!snapshot) {
      lines.push('Nothing collected yet — run "expiry-radar: Refresh Inventory".');
    } else if (shown === 0) {
      const hidden = this.all().length;
      lines.push(
        hidden > 0
          ? `${hidden} item(s) hidden by the current filter — "expiry-radar: Filter Items".`
          : 'Nothing expiring — or no sources were enabled.',
      );
    }

    // Never merely logged. An inventory that quietly lost a source reads
    // exactly like a clean estate, which is the failure this tool exists to
    // prevent, so it goes at the top of the pane in words.
    if (snapshot?.warnings.length) {
      lines.push(
        `⚠ ${snapshot.warnings.length} source(s) failed — this inventory is incomplete:`,
        ...snapshot.warnings.map((w) => `   ${w}`),
      );
    }
    return lines.length ? lines.join('\n') : undefined;
  }

  dispose(): void {
    this.changed.dispose();
  }
}
