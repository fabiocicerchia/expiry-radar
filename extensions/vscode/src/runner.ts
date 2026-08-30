/**
 * Locating expiry-radar and running it.
 *
 * The CLI renders one format per invocation and writes it to stdout, so the
 * extension asks for what it needs and reads the pipe: JSON for the panel, HTML
 * for the report tab, whatever the user picked for an export. Nothing is staged
 * through a temporary file except an export, which is a file by definition.
 */
import { spawn } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';

import { Settings } from './config';
import { log } from './log';
import { parseWarnings } from './parse';
import { Report } from './types';

export class RadarNotFoundError extends Error {}

/** The one-liner from the README — kept here so the notification can run it. */
export const INSTALL_COMMAND =
  'go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest';

export type Format = 'json' | 'html' | 'ical' | 'prometheus' | 'table';

export interface RunRequest {
  folder: vscode.WorkspaceFolder;
  format: Format;
  /** Hosts to probe on top of the settings — a one-off probe passes these. */
  endpoints?: string[];
  /** Domains to check on top of the settings. */
  domains?: string[];
  /** Skip the config file entirely: a one-off probe is about one host. */
  ignoreConfig?: boolean;
  /** Why this run started — shown in the log. */
  reason: string;
}

export interface RunResult {
  stdout: string;
  /** Sources that failed. A run can succeed and still be missing a source. */
  warnings: string[];
  /** The config file the run read, or '' when it ran on flags alone. */
  configPath: string;
  exitCode: number;
  durationMs: number;
}

/** A report of any plausible size is a few MB; this is a backstop, not a limit. */
const MAX_OUTPUT_CHARS = 32 * 1024 * 1024;
/**
 * Exit codes the CLI documents: 0 clean, 1 a `-fail-within` threshold was
 * breached, 2 bad usage or config, 3 partial results. Only 2 means there is no
 * report to read — the extension never passes `-fail-within`, but a user's
 * `extraArgs` might, and that run still produced a perfectly good inventory.
 */
const CODES_WITH_OUTPUT = new Set([0, 1, 3]);

let binaryCache = new Map<string, string>();

export function resetBinaryCache(): void {
  binaryCache = new Map();
}

function expand(p: string): string {
  return p.startsWith('~') ? path.join(os.homedir(), p.slice(1)) : p;
}

function isFile(candidate: string): boolean {
  try {
    return fs.statSync(candidate).isFile();
  } catch {
    return false;
  }
}

export function findOnPath(name: string): string {
  const exts =
    process.platform === 'win32' ? (process.env.PATHEXT ?? '.EXE;.CMD;.BAT').split(';') : [''];
  for (const dir of (process.env.PATH ?? '').split(path.delimiter)) {
    if (!dir) continue;
    for (const ext of exts) {
      const candidate = path.join(dir, name + ext);
      if (isFile(candidate)) return candidate;
    }
  }
  return '';
}

/** Every plausible location, most explicit first. */
function candidates(folder: vscode.WorkspaceFolder, s: Settings): string[] {
  const exe = process.platform === 'win32' ? 'expiry-radar.exe' : 'expiry-radar';
  const out: string[] = [];
  if (s.path) out.push(expand(s.path));
  // `make build` writes here, so a checkout of this repository is its own
  // best source of the binary — and the one most likely to be current.
  out.push(path.join(folder.uri.fsPath, 'bin', exe));
  const onPath = findOnPath('expiry-radar');
  if (onPath) out.push(onPath);
  // `go install` puts it in GOBIN, or GOPATH/bin, neither of which is
  // necessarily on the PATH of a GUI editor launched from a dock icon.
  const goBins = [
    process.env.GOBIN,
    process.env.GOPATH ? path.join(process.env.GOPATH, 'bin') : '',
    path.join(os.homedir(), 'go', 'bin'),
  ];
  for (const dir of goBins) if (dir) out.push(path.join(dir, exe));
  return out;
}

export function resolveBinary(folder: vscode.WorkspaceFolder, s: Settings): string {
  const key = folder.uri.toString();
  const cached = binaryCache.get(key);
  if (cached) return cached;

  for (const candidate of candidates(folder, s)) {
    if (!isFile(candidate)) continue;
    log().info(`using ${candidate}`);
    binaryCache.set(key, candidate);
    return candidate;
  }
  throw new RadarNotFoundError(
    'the expiry-radar binary was not found. Install it now?  It runs:  ' +
      `${INSTALL_COMMAND}  — or point "expiryRadar.path" at an existing build.`,
  );
}

/**
 * "expiry-radar is missing" told once, the same way, wherever it is noticed —
 * with the command that fixes it rather than a pointer to a document that has it.
 */
export async function promptInstall(message: string): Promise<void> {
  const choice = await vscode.window.showErrorMessage(
    `expiry-radar: ${message}`,
    'Install',
    'Copy command',
    'Open settings',
  );
  if (choice === 'Install') {
    const terminal = vscode.window.createTerminal('expiry-radar: install');
    terminal.show(true);
    terminal.sendText(INSTALL_COMMAND);
    // `go install` takes a moment; the next run should look again rather than
    // trust the "not found" we just cached.
    resetBinaryCache();
  } else if (choice === 'Copy command') {
    await vscode.env.clipboard.writeText(INSTALL_COMMAND);
  } else if (choice === 'Open settings') {
    await vscode.commands.executeCommand('workbench.action.openSettings', 'expiryRadar');
  }
}

/**
 * The config file this folder resolves to, or '' when there is none.
 *
 * Passing `-config` at a file that does not exist is harmless — the CLI treats
 * a missing default config as "no config" — but knowing which file was read is
 * what lets a diagnostic land on the line that asked for the item.
 */
export function resolveConfig(folder: vscode.WorkspaceFolder, s: Settings): string {
  const configured = s.configPath ? expand(s.configPath) : '';
  const candidate = configured
    ? path.isAbsolute(configured)
      ? configured
      : path.join(folder.uri.fsPath, configured)
    : path.join(folder.uri.fsPath, 'expiry-radar.json');
  return isFile(candidate) ? candidate : '';
}

/**
 * Whether this folder has anything to collect at all.
 *
 * Every source is opt-in: with no config file and nothing in the settings, a
 * run exits 2 with "no sources configured". That is the right answer to a
 * command, and completely wrong as a popup five seconds after every window
 * opens in a repository that has nothing to do with this tool.
 */
export function hasSources(folder: vscode.WorkspaceFolder, s: Settings): boolean {
  return resolveConfig(folder, s) !== '' || s.endpoints.length > 0 || s.domains.length > 0;
}

export function buildArgs(req: RunRequest, s: Settings, configPath: string): string[] {
  const args = ['-format', req.format];
  if (configPath) {
    args.push('-config', configPath);
  } else if (req.ignoreConfig) {
    // Not merely omitting the flag: `-config` defaults to `expiry-radar.json`,
    // resolved against the working directory, which is the workspace folder.
    // Omitting it on a machine that actually uses this tool would quietly
    // collect the whole estate alongside the one host being probed — slow, and
    // every credentialed source hit for a question about a single hostname.
    // An empty path stats as "does not exist", which the CLI already handles as
    // "no config"; the contract test pins that against the real binary.
    args.push('-config', '');
  }

  const endpoints = [...(req.ignoreConfig ? [] : s.endpoints), ...(req.endpoints ?? [])];
  const domains = [...(req.ignoreConfig ? [] : s.domains), ...(req.domains ?? [])];
  if (endpoints.length) args.push('-endpoints', endpoints.join(','));
  if (domains.length) args.push('-domains', domains.join(','));

  // Filtering happens in the CLI rather than in the panel: `-within` also caps
  // what the report and the export contain, and a panel that hid rows the
  // export still carried would be two different answers to one question.
  if (s.withinDays > 0) args.push('-within', String(s.withinDays));
  if (s.minPriority > 0) args.push('-min-priority', String(s.minPriority));
  args.push('-timeout', `${s.timeoutSeconds}s`);

  if (!req.ignoreConfig) args.push(...s.extraArgs);
  return args;
}

function exec(
  command: string,
  args: string[],
  opts: { cwd: string; timeoutMs: number; token?: vscode.CancellationToken },
): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    let child: ReturnType<typeof spawn>;
    try {
      // Its own process group: a collection has TLS dials, RDAP queries and
      // cloud SDK calls in flight, and signalling only the parent would leave
      // those sockets open in a process nothing is waiting for any more.
      child = spawn(command, args, {
        cwd: opts.cwd,
        env: process.env,
        detached: process.platform !== 'win32',
      });
    } catch (err) {
      reject(err);
      return;
    }

    const out: string[] = [];
    const errOut: string[] = [];
    let outChars = 0;
    let settled = false;
    let exited = false;

    const finish = (fn: () => void) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      cancelSub?.dispose();
      fn();
    };

    const signal = (sig: NodeJS.Signals) => {
      try {
        if (process.platform === 'win32') {
          spawn('taskkill', ['/pid', String(child.pid), '/T', '/F']).unref();
        } else if (child.pid) {
          process.kill(-child.pid, sig);
        }
      } catch {
        try {
          child.kill(sig);
        } catch {
          // Already gone.
        }
      }
    };

    // SIGTERM first: the CLI closes its sources on the signal, and a half-open
    // TLS dial left behind is a socket on somebody else's server too.
    const kill = () => {
      signal('SIGTERM');
      setTimeout(() => exited || signal('SIGKILL'), 3000).unref?.();
    };

    const timer = setTimeout(() => {
      kill();
      finish(() =>
        reject(new Error(`expiry-radar timed out after ${Math.round(opts.timeoutMs / 1000)}s`)),
      );
    }, opts.timeoutMs);

    const cancelSub = opts.token?.onCancellationRequested(() => {
      kill();
      finish(() => reject(new vscode.CancellationError()));
    });

    // Decoded by the stream rather than per chunk: a read boundary lands
    // mid-UTF-8 often enough on a large report, and decoding each half
    // separately corrupts the character that straddles it.
    child.stdout?.setEncoding('utf8');
    child.stderr?.setEncoding('utf8');
    child.stdout?.on('data', (s: string) => {
      if (outChars >= MAX_OUTPUT_CHARS) return;
      out.push(s);
      outChars += s.length;
    });
    child.stderr?.on('data', (s: string) => errOut.push(s));
    child.on('error', (err) => {
      exited = true;
      finish(() => reject(err));
    });
    child.on('exit', () => (exited = true));
    child.on('close', (code) =>
      finish(() => resolve({ code: code ?? -1, stdout: out.join(''), stderr: errOut.join('') })),
    );
  });
}

function tail(text: string, lines = 3): string {
  return text.trimEnd().split('\n').slice(-lines).join('\n');
}

export async function runRadar(
  req: RunRequest,
  s: Settings,
  token: vscode.CancellationToken,
): Promise<RunResult> {
  const binary = resolveBinary(req.folder, s);
  const configPath = req.ignoreConfig ? '' : resolveConfig(req.folder, s);
  const args = buildArgs(req, s, configPath);
  const started = Date.now();

  log().info(`collect (${req.reason}): ${binary} ${args.join(' ')}`);
  const { code, stdout, stderr } = await exec(binary, args, {
    cwd: req.folder.uri.fsPath,
    // A few seconds past the CLI's own budget, so its timeout wins and we get
    // its partial results instead of killing it a moment before it reports them.
    timeoutMs: (s.timeoutSeconds + 10) * 1000,
    token,
  });

  const warnings = parseWarnings(stderr);
  const durationMs = Date.now() - started;
  if (!CODES_WITH_OUTPUT.has(code) || !stdout.trim()) {
    // Exit 2 is bad usage or config; anything with no output at all is a real
    // failure whatever it claims. The CLI's own message is the useful part.
    const detail = tail(stderr) || tail(stdout) || 'no output';
    log().error(`no report (exit ${code})\n${stderr.trimEnd() || stdout.trimEnd()}`);
    throw new Error(`expiry-radar failed (exit ${code}): ${detail}`);
  }
  for (const warning of warnings) log().warn(`source failed: ${warning}`);
  log().info(`collected in ${(durationMs / 1000).toFixed(1)}s (exit ${code})`);
  return { stdout, warnings, configPath, exitCode: code, durationMs };
}

/** A `-format json` run, decoded. */
export async function collect(
  req: Omit<RunRequest, 'format'>,
  s: Settings,
  token: vscode.CancellationToken,
): Promise<{ report: Report; result: RunResult }> {
  const result = await runRadar({ ...req, format: 'json' }, s, token);
  let report: Report;
  try {
    report = JSON.parse(result.stdout) as Report;
  } catch (err) {
    throw new Error(`expiry-radar wrote a report that is not JSON: ${String(err)}`);
  }
  if (!Array.isArray(report.items)) throw new Error('expiry-radar wrote a report with no items');
  return { report, result };
}

/** Free-standing command runner, for the doctor. */
export async function probe(
  command: string,
  args: string[],
  cwd?: string,
  timeoutMs = 20_000,
): Promise<{ ok: boolean; output: string }> {
  try {
    const { code, stdout, stderr } = await exec(command, args, {
      cwd: cwd ?? process.cwd(),
      timeoutMs,
    });
    return { ok: code === 0, output: (stdout || stderr).trim() };
  } catch (err) {
    return { ok: false, output: err instanceof Error ? err.message : String(err) };
  }
}
