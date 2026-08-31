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

export const workspace = {
  workspaceFolders: [] as { uri: Uri; name: string; index: number }[],
  getConfiguration(section: string, _scope?: unknown) {
    return {
      get<T>(key: string): T | undefined {
        return testConfiguration.get(`${section}.${key}`) as T | undefined;
      },
    };
  },
  getWorkspaceFolder() {
    return undefined;
  },
  onDidSaveTextDocument() {
    return { dispose() {} };
  },
};

export const window = {
  state: { focused: true },
  createOutputChannel() {
    return {
      info() {},
      warn() {},
      error() {},
      debug() {},
      show() {},
      dispose() {},
    };
  },
  createStatusBarItem() {
    return { text: '', tooltip: undefined, show() {}, dispose() {} };
  },
  showErrorMessage: async () => undefined,
  showWarningMessage: async () => undefined,
  showInformationMessage: async () => undefined,
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
  executeCommand: async () => undefined,
  registerCommand: () => ({ dispose() {} }),
};

export const env = {
  clipboard: { writeText: async () => undefined },
  openExternal: async () => true,
};
