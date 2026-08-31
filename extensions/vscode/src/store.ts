/**
 * What the panel, the diagnostics and the status bar all read from.
 *
 * One snapshot per workspace folder, replaced wholesale. There is no merging to
 * do: a collection asks every configured source for everything it has, so a
 * newer run is not an update to the last one — it *is* the inventory, and
 * keeping rows the latest run did not return would be inventing an estate.
 */
import * as vscode from 'vscode';

import { Item, Snapshot } from './types';

function key(folder: vscode.WorkspaceFolder): string {
  return folder.uri.toString();
}

export class ResultStore {
  private snapshots = new Map<string, Snapshot>();

  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChange = this.changed.event;

  set(folder: vscode.WorkspaceFolder, snapshot: Snapshot): void {
    this.snapshots.set(key(folder), snapshot);
    this.changed.fire();
  }

  get(folder: vscode.WorkspaceFolder): Snapshot | undefined {
    return this.snapshots.get(key(folder));
  }

  items(folder: vscode.WorkspaceFolder): Item[] {
    return this.snapshots.get(key(folder))?.items ?? [];
  }

  /** Folders that have been collected, in workspace order. */
  folders(): vscode.WorkspaceFolder[] {
    return (vscode.workspace.workspaceFolders ?? []).filter((f) => this.snapshots.has(key(f)));
  }

  clear(): void {
    this.snapshots.clear();
    this.changed.fire();
  }

  dispose(): void {
    this.changed.dispose();
  }
}
