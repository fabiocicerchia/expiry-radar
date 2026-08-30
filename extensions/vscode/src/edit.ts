/**
 * Adding an entry to the config file.
 *
 * Text surgery rather than `JSON.parse` then `JSON.stringify`, for the same
 * reason locate.ts reads the file as text: a round-trip through the parser
 * reformats the whole document, reorders nothing but re-indents everything, and
 * drops the ordering somebody chose. An operator who added one host should get
 * a one-line diff, not a rewritten file.
 */
import { arraySpan } from './locate';
import { Kind } from './types';

/** What can be recorded, as opposed to discovered. */
export type EntryKind = 'endpoint' | 'domain' | 'manual';

export interface ManualEntry {
  name: string;
  kind: Kind;
  /** RFC 3339, or YYYY-MM-DD — whatever `source.ManualItem` accepts. */
  expires: string;
  namespace?: string;
}

/** The config key each kind of entry lives under. */
export const ARRAY_FOR: Record<EntryKind, string> = {
  endpoint: 'endpoints',
  domain: 'domains',
  manual: 'manual',
};

/** The CLI's `source.Kinds`, in the order `internal/rank` weights them. */
export const MANUAL_KINDS: { kind: Kind; label: string; hint: string }[] = [
  { kind: 'domain', label: 'Domain', hint: 'a registration — the whole estate, including mail' },
  { kind: 'intermediate_ca', label: 'Intermediate CA', hint: 'every leaf it signed, at once' },
  { kind: 'tls_cert', label: 'TLS certificate', hint: 'a code-signing or client cert, say' },
  { kind: 'iam_access_key', label: 'IAM access key', hint: 'a key rotated by hand' },
  { kind: 'secret', label: 'Secret', hint: 'an API token, a password' },
  { kind: 'vault_lease', label: 'Vault lease', hint: 'a lease nothing enumerates' },
];

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/;
// RFC 3339 proper. Deliberately not `Date.parse`, which accepts `03/01/2027`
// and a good deal else that Go's time.Parse rejects — writing one of those into
// the config would produce an editor that happily records a date the CLI then
// refuses to load.
const RFC_3339 = /^(\d{4})-(\d{2})-(\d{2})[Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$/;

/** Whether the calendar actually has this day. */
function isRealDate(y: number, m: number, d: number): boolean {
  const date = new Date(Date.UTC(y, m - 1, d));
  return date.getUTCFullYear() === y && date.getUTCMonth() === m - 1 && date.getUTCDate() === d;
}

/**
 * The same rule `source.ManualItem.ExpiresAt` applies, so the editor rejects
 * what the CLI would reject rather than writing a config that fails to load.
 */
export function invalidExpires(value: string): string | undefined {
  const trimmed = value.trim();
  if (!trimmed) return 'A date is required.';
  const parts = DATE_ONLY.exec(trimmed) ?? RFC_3339.exec(trimmed);
  if (!parts) return 'Use YYYY-MM-DD, or a full RFC 3339 timestamp.';
  // The shape is right; the calendar still has to have the day. `Date.UTC`
  // rolls 2027-02-31 over to 3 March rather than failing, which would record a
  // deadline nobody chose.
  const [, y, m, d] = parts;
  if (!isRealDate(Number(y), Number(m), Number(d))) return `${trimmed} is not a real date.`;
  if (Number.isNaN(Date.parse(trimmed))) return `${trimmed} is not a valid time.`;
  return undefined;
}

/** One entry, rendered as the CLI's config expects it. */
export function renderEntry(kind: EntryKind, value: string | ManualEntry): string {
  if (kind === 'domain') return JSON.stringify(String(value).trim());
  if (kind === 'endpoint') return JSON.stringify({ host: String(value).trim() });
  const entry = value as ManualEntry;
  const out: Record<string, string> = {
    name: entry.name.trim(),
    kind: String(entry.kind),
    expires: entry.expires.trim(),
  };
  if (entry.namespace?.trim()) out.namespace = entry.namespace.trim();
  return JSON.stringify(out);
}

/** The indentation of the line `offset` sits on. */
function indentAt(text: string, offset: number): string {
  const lineStart = text.lastIndexOf('\n', offset - 1) + 1;
  return /^[ \t]*/.exec(text.slice(lineStart, offset))?.[0] ?? '';
}

/**
 * Append `entry` to the array under `key`, creating the array — and the object
 * around it — when they are not there yet.
 *
 * Returns the new text, and the 1-based line the entry landed on so the caller
 * can put the cursor there. A config nobody can find the new line in is a
 * config somebody edits twice.
 */
export function addToArray(text: string, key: string, entry: string): { text: string; line: number } {
  const span = arraySpan(text, key);
  if (span) return appendToExisting(text, span, entry);
  return addKey(text, key, entry);
}

function lineOf(text: string, offset: number): number {
  let line = 1;
  for (let i = 0; i < offset; i += 1) if (text.charCodeAt(i) === 10) line += 1;
  return line;
}

function appendToExisting(
  text: string,
  [open, close]: [number, number],
  entry: string,
): { text: string; line: number } {
  const body = text.slice(open + 1, close - 1);
  const empty = body.trim() === '';
  // A one-line array stays a one-line array: re-flowing `["a", "b"]` across
  // three lines to add a third element is not the diff anybody asked for.
  const inline = !body.includes('\n');

  if (empty && inline) {
    const insertAt = close - 1;
    const next = text.slice(0, insertAt) + entry + text.slice(insertAt);
    return { text: next, line: lineOf(next, insertAt) };
  }
  if (inline) {
    const insertAt = close - 1;
    const next = text.slice(0, insertAt) + `, ${entry}` + text.slice(insertAt);
    return { text: next, line: lineOf(next, insertAt) };
  }

  // Multi-line: land on a line of its own, indented like its siblings, with a
  // comma added to whatever was previously last.
  const lastContent = open + 1 + body.replace(/\s+$/, '').length;
  const indent = empty
    ? indentAt(text, open) + '  '
    : indentAt(text, lastContent - body.trim().split('\n').pop()!.length);
  const closeIndent = indentAt(text, close - 1);
  if (empty) {
    const next = `${text.slice(0, open + 1)}\n${indent}${entry}\n${closeIndent}${text.slice(close - 1)}`;
    return { text: next, line: lineOf(next, open + 2) };
  }
  const next = `${text.slice(0, lastContent)},\n${indent}${entry}${text.slice(lastContent)}`;
  return { text: next, line: lineOf(next, lastContent + 2) };
}

/** No such array yet — add the key to the top-level object, or make one. */
function addKey(text: string, key: string, entry: string): { text: string; line: number } {
  const trimmed = text.trim();
  if (!trimmed || !trimmed.startsWith('{')) {
    const next = `{\n  "${key}": [${entry}]\n}\n`;
    return { text: next, line: 2 };
  }

  const close = text.lastIndexOf('}');
  const before = text.slice(0, close).replace(/\s+$/, '');
  // An empty object has nothing to separate the new key from.
  const needsComma = before.trimEnd().endsWith(',') === false && before.trim() !== '{';
  const indent = indentAt(text, close) + '  ';
  const next =
    `${before}${needsComma ? ',' : ''}\n${indent}"${key}": [${entry}]\n` + text.slice(close);
  return { text: next, line: lineOf(next, before.length + 2) };
}
