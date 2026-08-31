import * as vscode from 'vscode';

export type Trigger = 'onConfigSave' | 'onConfigSaveAndInterval' | 'interval' | 'manual';

export interface Settings {
  /** The binary. Empty means auto-detect. */
  path: string;
  /** `-config`. Empty means "expiry-radar.json in the folder, if it exists". */
  configPath: string;
  endpoints: string[];
  domains: string[];
  extraArgs: string[];
  trigger: Trigger;
  scanOnStartup: boolean;
  intervalMinutes: number;
  debounceMs: number;
  timeoutSeconds: number;
  diagnosticsEnabled: boolean;
  warnWithinDays: number;
  infoWithinDays: number;
  withinDays: number;
  minPriority: number;
  statusWarnWithinDays: number;
}

export function readSettings(scope?: vscode.Uri): Settings {
  const c = vscode.workspace.getConfiguration('expiryRadar', scope ?? null);
  const get = <T>(key: string, fallback: T): T => c.get<T>(key) ?? fallback;
  const list = (key: string): string[] =>
    get<string[]>(key, [])
      .map((s) => String(s).trim())
      .filter(Boolean);

  const warnWithinDays = Math.max(0, get('diagnostics.warnWithinDays', 14));
  return {
    path: get('path', '').trim(),
    configPath: get('configPath', '').trim(),
    endpoints: list('endpoints'),
    domains: list('domains'),
    extraArgs: get<string[]>('extraArgs', []),
    trigger: get<Trigger>('scan.trigger', 'onConfigSaveAndInterval'),
    scanOnStartup: get('scan.onStartup', true),
    intervalMinutes: Math.max(5, get('scan.intervalMinutes', 60)),
    debounceMs: Math.max(250, get('scan.debounceMs', 1500)),
    timeoutSeconds: Math.max(10, get('scan.timeoutSeconds', 120)),
    diagnosticsEnabled: get('diagnostics.enabled', true),
    warnWithinDays,
    // A window that ends before the warning window would silently drop the
    // warnings it is meant to sit outside of.
    infoWithinDays: Math.max(warnWithinDays, get('diagnostics.infoWithinDays', 30)),
    withinDays: Math.max(0, get('view.withinDays', 0)),
    minPriority: Math.min(1, Math.max(0, get('view.minPriority', 0))),
    statusWarnWithinDays: Math.max(0, get('status.warnWithinDays', 14)),
  };
}
