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

interface ConfigShape {
  endpoints?: unknown[];
  domains?: unknown[];
  k8s?: { enabled?: boolean; server?: string };
  vault?: { enabled?: boolean; addr?: string };
  aws?: { enabled?: boolean; region?: string; profile?: string };
}

export async function runDoctor(folder: vscode.WorkspaceFolder, s: Settings): Promise<void> {
  const lines: string[] = [];
  // Collected rather than folded into a running worst-so-far: a closure that
  // reassigns an outer variable is exactly what the compiler cannot follow.
  const levels: Level[] = [];
  const say = (level: Level, text: string, ...detail: string[]) => {
    lines.push(`${MARK[level]} ${text}`);
    for (const d of detail) lines.push(`    ${d}`);
    levels.push(level);
  };

  lines.push(`expiry-radar doctor — ${folder.name}`, '');

  let binary = '';
  try {
    binary = resolveBinary(folder, s);
  } catch (err) {
    if (err instanceof RadarNotFoundError) {
      say('error', 'the expiry-radar binary was not found', INSTALL_COMMAND, 'or set "expiryRadar.path"');
    } else {
      throw err;
    }
  }

  if (binary) {
    // There is no --version; the usage text is the cheapest proof that the
    // thing on disk is the CLI we are about to trust with the panel.
    const help = await probe(binary, ['-h'], folder.uri.fsPath);
    if (help.output.includes('expiry-radar')) say('ok', `runs: ${binary}`);
    else say('warn', `${binary} ran, but does not look like expiry-radar`, help.output.split('\n')[0] ?? '');
  }

  const configPath = binary ? resolveConfig(folder, s) : '';
  if (!configPath) {
    const expected = s.configPath || 'expiry-radar.json';
    if (s.endpoints.length || s.domains.length) {
      say('info', `no config file at ${expected} — running on settings alone`);
    } else {
      say(
        'warn',
        `no config file at ${expected}, and no endpoints or domains in settings`,
        'Nothing is enabled implicitly: without one of these there are no sources to run.',
        'Copy expiry-radar.example.json to expiry-radar.json to start.',
      );
    }
  } else {
    say('ok', `config: ${path.relative(folder.uri.fsPath, configPath) || configPath}`);
    await describeConfig(configPath, say);
  }

  if (s.endpoints.length) say('info', `settings add ${s.endpoints.length} endpoint(s)`);
  if (s.domains.length) say('info', `settings add ${s.domains.length} domain(s)`);
  if (s.withinDays > 0) {
    say('info', `"expiryRadar.view.withinDays" is ${s.withinDays} — anything further out is not collected`);
  }
  if (s.minPriority > 0) {
    say('info', `"expiryRadar.view.minPriority" is ${s.minPriority} — lower-ranked items are not collected`);
  }

  lines.push('');
  const channel = log();
  for (const line of lines) channel.info(line);
  channel.show(true);

  const worst: Level = levels.includes('error') ? 'error' : levels.includes('warn') ? 'warn' : 'ok';
  const summary =
    worst === 'error'
      ? 'expiry-radar: the doctor found something that stops it running — see the log.'
      : worst === 'warn'
        ? 'expiry-radar: the doctor found something that will limit results — see the log.'
        : 'expiry-radar: everything the doctor checks looks fine.';
  void vscode.window.showInformationMessage(summary);
}

async function describeConfig(
  configPath: string,
  say: (level: Level, text: string, ...detail: string[]) => void,
): Promise<void> {
  let parsed: ConfigShape;
  try {
    parsed = JSON.parse(await fs.promises.readFile(configPath, 'utf8')) as ConfigShape;
  } catch (err) {
    say('error', 'the config file is not valid JSON', String(err));
    return;
  }

  const endpoints = parsed.endpoints?.length ?? 0;
  const domains = parsed.domains?.length ?? 0;
  if (endpoints) say('ok', `${endpoints} endpoint(s) to probe over TLS`);
  if (domains) say('ok', `${domains} domain(s) to check via RDAP`);

  // Credentials never come from the config file — they come from the
  // environment — so an enabled source with an empty environment is the most
  // common way to get a clean-looking report that is missing an entire account.
  if (parsed.k8s?.enabled) {
    const server = parsed.k8s.server;
    if (server) say('ok', `kubernetes: ${server}`);
    else if (process.env.KUBERNETES_SERVICE_HOST) say('ok', 'kubernetes: in-cluster');
    else {
      say(
        'warn',
        'kubernetes is enabled with no server, and this is not a cluster pod',
        'Run `kubectl proxy` and set "server": "http://127.0.0.1:8001", or run in-cluster.',
      );
    }
  }
  if (parsed.vault?.enabled) {
    const addr = parsed.vault.addr || process.env.VAULT_ADDR || '';
    if (!addr) say('warn', 'vault is enabled with no addr and no $VAULT_ADDR');
    else if (!process.env.VAULT_TOKEN) {
      say('warn', `vault is enabled (${addr}) but $VAULT_TOKEN is not set in the editor's environment`);
    } else say('ok', `vault: ${addr}`);
  }
  if (parsed.aws?.enabled) {
    const credentialed =
      process.env.AWS_ACCESS_KEY_ID ||
      process.env.AWS_PROFILE ||
      process.env.AWS_ROLE_ARN ||
      process.env.AWS_WEB_IDENTITY_TOKEN_FILE ||
      parsed.aws.profile;
    if (credentialed) say('ok', `aws: ${parsed.aws.region || '$AWS_REGION'}`);
    else {
      say(
        'warn',
        'aws is enabled but the editor has no AWS credentials in its environment',
        'The credential chain is read from the process the editor was launched from.',
      );
    }
  }

  if (!endpoints && !domains && !parsed.k8s?.enabled && !parsed.vault?.enabled && !parsed.aws?.enabled) {
    say('error', 'the config file enables no sources at all', 'Every source is opt-in; nothing runs implicitly.');
  }
}
