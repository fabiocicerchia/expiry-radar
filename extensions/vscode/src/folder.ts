/**
 * Which folder a command is about, and its settings.
 *
 * Every command starts with the same question, and answering it two ways in two
 * places would mean a collection and the diagnostics it publishes could end up
 * describing different folders.
 */
import * as vscode from 'vscode';

import { readSettings, Settings } from './config';

/** The folder the active editor is in, or the first one the window has open. */
export function primaryFolder(): vscode.WorkspaceFolder | undefined {
  const active = vscode.window.activeTextEditor?.document.uri;
  return (
    (active ? vscode.workspace.getWorkspaceFolder(active) : undefined) ??
    vscode.workspace.workspaceFolders?.[0]
  );
}

export function settingsFor(folder?: vscode.WorkspaceFolder): Settings {
  return readSettings(folder?.uri);
}
