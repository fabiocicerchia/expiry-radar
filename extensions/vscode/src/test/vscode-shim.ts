/**
 * Just enough of the `vscode` module for the unit tests.
 *
 * The tests run under plain `node --test`, with no editor to import from, and
 * esbuild aliases the `vscode` import to this file. Only the surface the tested
 * modules actually touch is here — anything else should stay untested rather
 * than grow a second, fictional editor to be tested against.
 */

export class EventEmitter<T> {
  private listeners: ((value: T) => void)[] = [];

  readonly event = (listener: (value: T) => void): { dispose(): void } => {
    this.listeners.push(listener);
    return {
      dispose: () => {
        this.listeners = this.listeners.filter((l) => l !== listener);
      },
    };
  };

  fire(value: T): void {
    for (const listener of [...this.listeners]) listener(value);
  }

  dispose(): void {
    this.listeners = [];
  }
}

export class CancellationError extends Error {
  constructor() {
    super('Canceled');
    this.name = 'Canceled';
  }
}

export class CancellationTokenSource {
  private emitter = new EventEmitter<void>();
  readonly token = {
    isCancellationRequested: false,
    onCancellationRequested: this.emitter.event,
  };

  cancel(): void {
    if (this.token.isCancellationRequested) return;
    this.token.isCancellationRequested = true;
    this.emitter.fire();
  }

  dispose(): void {
    this.emitter.dispose();
  }
}

export class Uri {
  private constructor(
    readonly scheme: string,
    readonly fsPath: string,
  ) {}

  static file(fsPath: string): Uri {
    return new Uri('file', fsPath);
  }

  static parse(value: string): Uri {
    return new Uri(value.split(':')[0] ?? 'file', value);
  }

  toString(): string {
    return `${this.scheme}://${this.fsPath}`;
  }
}

export class Position {
  constructor(
    readonly line: number,
    readonly character: number,
  ) {}
}

export class Range {
  readonly start: Position;
  readonly end: Position;

  constructor(startLine: number, startColumn: number, endLine: number, endColumn: number) {
    this.start = new Position(startLine, startColumn);
    this.end = new Position(endLine, endColumn);
  }
}

export class Selection {
  constructor(
    readonly anchor: Position,
    readonly active: Position,
  ) {}

  get isEmpty(): boolean {
    return this.anchor.line === this.active.line && this.anchor.character === this.active.character;
  }
}

export enum DiagnosticSeverity {
  Error = 0,
  Warning = 1,
  Information = 2,
  Hint = 3,
}

export class Diagnostic {
  source = '';
  code: unknown = undefined;

  constructor(
    readonly range: Range,
    readonly message: string,
    readonly severity: DiagnosticSeverity,
  ) {}
}

export class MarkdownString {
  value = '';

  appendMarkdown(text: string): this {
    this.value += text;
    return this;
  }
}

export class ThemeIcon {
  constructor(
    readonly id: string,
    readonly color?: ThemeColor,
  ) {}
}

export class ThemeColor {
  constructor(readonly id: string) {}
}

export enum TreeItemCollapsibleState {
  None = 0,
  Collapsed = 1,
  Expanded = 2,
}

export class TreeItem {
  id?: string;
  description?: string;
  iconPath?: unknown;
  resourceUri?: Uri;
  tooltip?: unknown;
  contextValue?: string;
  command?: unknown;

  constructor(
    readonly label: string,
    readonly collapsibleState: TreeItemCollapsibleState,
  ) {}
}

export enum QuickPickItemKind {
  Separator = -1,
  Default = 0,
}

export enum StatusBarAlignment {
  Left = 1,
  Right = 2,
}

export enum ProgressLocation {
  Notification = 15,
  Window = 10,
}

/** Settings the tests set directly, read back through `getConfiguration`. */
export const testConfiguration = new Map<string, unknown>();

/**
 * What the extension did to the editor, in order.
 *
 * `activate()` is only observable through the editor it wires itself into, so
 * the shim records rather than ignores: which command ids exist, what was asked
 * of the user, what the log was told. A test asserts on these lists, never on
 * the extension's own internals.
 */
export const registeredCommands = new Map<string, (...args: never[]) => unknown>();
export const executedCommands: { command: string; args: unknown[] }[] = [];
/** Every prompt and notification, as `<kind>: <title or message>`. */
export const prompts: string[] = [];
/** Every line the output channel was given. */
export const logged: string[] = [];
/**
 * What the next prompt returns, in order. A function is called with the items
 * offered, so a test can pick one without restating it.
 */
export const answers: unknown[] = [];

export function resetShim(): void {
  registeredCommands.clear();
  executedCommands.length = 0;
  prompts.length = 0;
  logged.length = 0;
  answers.length = 0;
  testConfiguration.clear();
  workspace.workspaceFolders = [];
  window.activeTextEditor = undefined;
}

function answer(kind: string, label: string, items?: unknown): unknown {
  prompts.push(`${kind}: ${label}`);
  const next = answers.shift();
  return typeof next === 'function' ? (next as (i: unknown) => unknown)(items) : next;
}

export const workspace = {
  workspaceFolders: [] as { uri: Uri; name: string; index: number }[],
  getConfiguration(section: string, _scope?: unknown) {
    return {
      get<T>(key: string): T | undefined {
        return testConfiguration.get(`${section}.${key}`) as T | undefined;
      },
    };
  },
  getWorkspaceFolder(uri?: Uri) {
    return workspace.workspaceFolders.find((f) => uri?.fsPath.startsWith(f.uri.fsPath));
  },
  async openTextDocument(target: string | Uri) {
    const uri = typeof target === 'string' ? Uri.file(target) : target;
    return { uri, fileName: uri.fsPath };
  },
  onDidSaveTextDocument() {
    return { dispose() {} };
  },
  onDidChangeConfiguration() {
    return { dispose() {} };
  },
  onDidChangeWorkspaceFolders() {
    return { dispose() {} };
  },
};

export const window = {
  state: { focused: true },
  activeTextEditor: undefined as unknown,
  createOutputChannel() {
    return {
      info(message: string) {
        logged.push(message);
      },
      warn(message: string) {
        logged.push(message);
      },
      error(message: string) {
        logged.push(message);
      },
      debug(message: string) {
        logged.push(message);
      },
      show() {},
      dispose() {},
    };
  },
  createStatusBarItem() {
    return {
      text: '',
      name: undefined as string | undefined,
      command: undefined as string | undefined,
      tooltip: undefined as unknown,
      backgroundColor: undefined as unknown,
      show() {},
      hide() {},
      dispose() {},
    };
  },
  createTreeView(viewId: string, _options?: unknown) {
    return { viewId, reveal: async () => undefined, dispose() {} };
  },
  createTerminal() {
    return { show() {}, sendText() {}, dispose() {} };
  },
  async showTextDocument(document: unknown) {
    return {
      document,
      selection: undefined as unknown,
      revealRange() {},
    };
  },
  async withProgress<T>(
    _options: unknown,
    task: (progress: { report(): void }, token: unknown) => Thenable<T>,
  ): Promise<T> {
    return task({ report() {} }, new CancellationTokenSource().token);
  },
  onDidChangeActiveTextEditor() {
    return { dispose() {} };
  },
  showErrorMessage: async (message: string) => answer('error', message),
  showWarningMessage: async (message: string) => answer('warning', message),
  showInformationMessage: async (message: string) => answer('information', message),
  showQuickPick: async (items: unknown, options?: { title?: string }) =>
    answer('quickPick', options?.title ?? '', await items),
  showInputBox: async (options?: { title?: string; prompt?: string }) =>
    answer('inputBox', options?.title ?? options?.prompt ?? ''),
  showSaveDialog: async (options?: { title?: string }) => answer('saveDialog', options?.title ?? ''),
};

export const languages = {
  createDiagnosticCollection() {
    const entries = new Map<string, Diagnostic[]>();
    return {
      set(pairs: [Uri, Diagnostic[]][]) {
        entries.clear();
        for (const [uri, diagnostics] of pairs) entries.set(uri.fsPath, diagnostics);
      },
      clear() {
        entries.clear();
      },
      dispose() {
        entries.clear();
      },
      /** Test-only: what the last publish wrote. */
      _entries: entries,
    };
  },
};

export const commands = {
  async executeCommand(command: string, ...args: unknown[]) {
    executedCommands.push({ command, args });
    const handler = registeredCommands.get(command);
    return handler ? await handler(...(args as never[])) : undefined;
  },
  registerCommand(command: string, handler: (...args: never[]) => unknown) {
    registeredCommands.set(command, handler);
    return {
      dispose() {
        registeredCommands.delete(command);
      },
    };
  },
};

export const env = {
  clipboard: { writeText: async () => undefined },
  openExternal: async () => true,
};
