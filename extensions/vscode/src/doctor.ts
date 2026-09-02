/**
 * "Why did that come back empty?"
 *
 * Deliberately not a source-by-source inventory: a collection already reports
 * every source it ran, and a second copy of that list here would drift. What a
 * collection cannot tell you is why it produced nothing at all — no binary, no
 * config, a source enabled with no credentials in the environment to reach it —
 * so that is what this checks.
 */
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';

import { Settings } from './config';
import { log } from './log';
import { INSTALL_COMMAND, probe, RadarNotFoundError, resolveBinary, resolveConfig } from './runner';

type Level = 'ok' | 'warn' | 'error' | 'info';

const MARK: Record<Level, string> = { ok: '✓', warn: '!', error: '✗', info: '·' };

const SUMMARY: Record<'ok' | 'warn' | 'error', string> = {
  error: 'expiry-radar: the doctor found something that stops it running — see the log.',
  warn: 'expiry-radar: the doctor found something that will limit results — see the log.',
  ok: 'expiry-radar: everything the doctor checks looks fine.',
};

interface ConfigShape {
  endpoints?: unknown[];
  domains?: unknown[];
  k8s?: { enabled?: boolean; server?: string };
  vault?: { enabled?: boolean; addr?: string };
  aws?: { enabled?: boolean; region?: string; profile?: string };
}

/**
 * The report as it is written.
 *
 * The severities are collected rather than folded into a running worst-so-far:
 * each check just says what it found, and the summary is decided once, at the
 * end, from the whole list.
 */
class Findings {
  readonly lines: string[] = [];
  private readonly levels: Level[] = [];

  say(level: Level, text: string, ...detail: string[]): void {
    this.lines.push(`${MARK[level]} ${text}`);
    for (const d of detail) this.lines.push(`    ${d}`);
    this.levels.push(level);
  }

  get summary(): string {
    if (this.levels.includes('error')) return SUMMARY.error;
    return this.levels.includes('warn') ? SUMMARY.warn : SUMMARY.ok;
  }
}

export async function runDoctor(folder: vscode.WorkspaceFolder, s: Settings): Promise<void> {
  const found = new Findings();
  found.lines.push(`expiry-radar doctor — ${folder.name}`, '');

  const binary = locateBinary(folder, s, found);
  if (binary) await checkBinaryIsRadar(binary, folder, found);
  // Only when there is a binary: without one, which config it would have read
  // is not the thing standing between the operator and a result.
  if (binary) await checkConfig(folder, s, found);
  noteSettings(s, found);
  found.lines.push('');

  const channel = log();
  for (const line of found.lines) channel.info(line);
  channel.show(true);
  void vscode.window.showInformationMessage(found.summary);
}

/** The binary, or '' with the reason it could not be found already reported. */
function locateBinary(folder: vscode.WorkspaceFolder, s: Settings, found: Findings): string {
  try {
    return resolveBinary(folder, s);
  } catch (err) {
    if (!(err instanceof RadarNotFoundError)) throw err;
    found.say('error', 'the expiry-radar binary was not found', INSTALL_COMMAND, 'or set "expiryRadar.path"');
    return '';
  }
}

async function checkBinaryIsRadar(
  binary: string,
  folder: vscode.WorkspaceFolder,
  found: Findings,
): Promise<void> {
  // There is no --version; the usage text is the cheapest proof that the thing
  // on disk is the CLI we are about to trust with the panel.
  const help = await probe(binary, ['-h'], folder.uri.fsPath);
  if (help.output.includes('expiry-radar')) found.say('ok', `runs: ${binary}`);
  else found.say('warn', `${binary} ran, but does not look like expiry-radar`, help.output.split('\n')[0] ?? '');
}

async function checkConfig(
  folder: vscode.WorkspaceFolder,
  s: Settings,
  found: Findings,
): Promise<void> {
  const configPath = resolveConfig(folder, s);
  if (configPath) {
    found.say('ok', `config: ${path.relative(folder.uri.fsPath, configPath) || configPath}`);
    await describeConfig(configPath, found);
    return;
  }
  const expected = s.configPath || 'expiry-radar.json';
  if (s.endpoints.length || s.domains.length) {
    found.say('info', `no config file at ${expected} — running on settings alone`);
    return;
  }
  found.say(
    'warn',
    `no config file at ${expected}, and no endpoints or domains in settings`,
    'Nothing is enabled implicitly: without one of these there are no sources to run.',
    'Copy expiry-radar.example.json to expiry-radar.json to start.',
  );
}

/** Settings that quietly shrink what a collection returns. */
function noteSettings(s: Settings, found: Findings): void {
  if (s.endpoints.length) found.say('info', `settings add ${s.endpoints.length} endpoint(s)`);
  if (s.domains.length) found.say('info', `settings add ${s.domains.length} domain(s)`);
  if (s.withinDays > 0) {
    found.say('info', `"expiryRadar.view.withinDays" is ${s.withinDays} — anything further out is not collected`);
  }
  if (s.minPriority > 0) {
    found.say('info', `"expiryRadar.view.minPriority" is ${s.minPriority} — lower-ranked items are not collected`);
  }
}

async function describeConfig(configPath: string, found: Findings): Promise<void> {
  let parsed: ConfigShape;
  try {
    parsed = JSON.parse(await fs.promises.readFile(configPath, 'utf8')) as ConfigShape;
  } catch (err) {
    found.say('error', 'the config file is not valid JSON', String(err));
    return;
  }

  const endpoints = parsed.endpoints?.length ?? 0;
  const domains = parsed.domains?.length ?? 0;
  if (endpoints) found.say('ok', `${endpoints} endpoint(s) to probe over TLS`);
  if (domains) found.say('ok', `${domains} domain(s) to check via RDAP`);

  // Credentials never come from the config file — they come from the
  // environment — so an enabled source with an empty environment is the most
  // common way to get a clean-looking report that is missing an entire account.
  // Every describe* reports whether its source was enabled at all, so the
  // "nothing is enabled" verdict is the same three answers, not a second read
  // of the same three flags.
  const discovered = [
    describeKubernetes(parsed.k8s, found),
    describeVault(parsed.vault, found),
    describeAws(parsed.aws, found),
  ].some(Boolean);
  if (!endpoints && !domains && !discovered) {
    found.say('error', 'the config file enables no sources at all', 'Every source is opt-in; nothing runs implicitly.');
  }
}

function describeKubernetes(k8s: ConfigShape['k8s'], found: Findings): boolean {
  if (!k8s?.enabled) return false;
  if (k8s.server) found.say('ok', `kubernetes: ${k8s.server}`);
  else if (process.env.KUBERNETES_SERVICE_HOST) found.say('ok', 'kubernetes: in-cluster');
  else {
    found.say(
      'warn',
      'kubernetes is enabled with no server, and this is not a cluster pod',
      'Run `kubectl proxy` and set "server": "http://127.0.0.1:8001", or run in-cluster.',
    );
  }
  return true;
}

function describeVault(vault: ConfigShape['vault'], found: Findings): boolean {
  if (!vault?.enabled) return false;
  const addr = vault.addr || process.env.VAULT_ADDR || '';
  if (!addr) found.say('warn', 'vault is enabled with no addr and no $VAULT_ADDR');
  else if (!process.env.VAULT_TOKEN) {
    found.say('warn', `vault is enabled (${addr}) but $VAULT_TOKEN is not set in the editor's environment`);
  } else found.say('ok', `vault: ${addr}`);
  return true;
}

function describeAws(aws: ConfigShape['aws'], found: Findings): boolean {
  if (!aws?.enabled) return false;
  const credentialed =
    process.env.AWS_ACCESS_KEY_ID ||
    process.env.AWS_PROFILE ||
    process.env.AWS_ROLE_ARN ||
    process.env.AWS_WEB_IDENTITY_TOKEN_FILE ||
    aws.profile;
  if (credentialed) found.say('ok', `aws: ${aws.region || '$AWS_REGION'}`);
  else {
    found.say(
      'warn',
      'aws is enabled but the editor has no AWS credentials in its environment',
      'The credential chain is read from the process the editor was launched from.',
    );
  }
  return true;
}
