/**
 * Where an item was declared.
 *
 * A finding in gandalf points at the line that caused it; the equivalent here is
 * the line in `expiry-radar.json` that asked for the host or the domain. Only
 * the two file-declared sources have one — a certificate found on an Ingress or
 * a key found in IAM was never written down anywhere in the repository, and
 * pointing those at a config line would be a lie. They live in the panel only.
 *
 * The config is read as text rather than through `JSON.parse`, because a
 * position is exactly what parsing throws away, and a hand-rolled scanner over
 * two known arrays is a great deal less code than a position-preserving parser.
 */
import { Origin } from './types';

/** 1-based line and column of an offset, as an editor counts them. */
function positionOf(text: string, offset: number): { line: number; column: number } {
  let line = 1;
  let start = 0;
  for (let i = 0; i < offset; i += 1) {
    if (text.charCodeAt(i) === 10) {
      line += 1;
      start = i + 1;
    }
  }
  return { line, column: offset - start + 1 };
}

/**
 * The `[...]` that follows `"key"`, as offsets into the text. String-aware, so
 * a bracket inside a value cannot close the array early.
 *
 * Exported for edit.ts, which appends to the same arrays this one reads.
 */
export function arraySpan(text: string, key: string): [number, number] | undefined {
  const at = text.search(new RegExp(`"${key}"\\s*:\\s*\\[`));
  if (at < 0) return undefined;
  const open = text.indexOf('[', at);
  let depth = 0;
  let inString = false;
  for (let i = open; i < text.length; i += 1) {
    const ch = text[i];
    if (inString) {
      if (ch === '\\') i += 1;
      else if (ch === '"') inString = false;
      continue;
    }
    if (ch === '"') inString = true;
    else if (ch === '[' || ch === '{') depth += 1;
    else if (ch === ']' || ch === '}') {
      depth -= 1;
      if (depth === 0) return [open, i + 1];
    }
  }
  return undefined; // Unterminated: the file is mid-edit, so claim nothing.
}

/**
 * Item name -> where it is declared, for one config file.
 *
 * Keyed by the item's `name` exactly as the CLI reports it: the TLS source
 * names a certificate after the `host` it was configured with (port and all),
 * and the RDAP source names a domain after the string in `domains`. That
 * equality is the whole mapping — no normalisation, because a host that does
 * not round-trip is a host we would be guessing about.
 */
export function declaredIn(file: string, text: string): Map<string, Origin> {
  const found = new Map<string, Origin>();
  const record = (value: string, offset: number) => {
    // First declaration wins: a duplicated host is one item, and the earlier
    // line is the one somebody will look at.
    if (value && !found.has(value)) found.set(value, { file, ...positionOf(text, offset) });
  };

  const endpoints = arraySpan(text, 'endpoints');
  if (endpoints) {
    const [from, to] = endpoints;
    const section = text.slice(from, to);
    const host = /"host"\s*:\s*"((?:[^"\\]|\\.)*)"/g;
    for (let m = host.exec(section); m; m = host.exec(section)) {
      // Point at the value, not at the key: that is what the squiggle is about.
      record(unescape(m[1]), from + m.index + m[0].lastIndexOf('"', m[0].length - 2));
    }
  }

  const domains = arraySpan(text, 'domains');
  if (domains) {
    const [from, to] = domains;
    const section = text.slice(from, to);
    const entry = /"((?:[^"\\]|\\.)*)"/g;
    for (let m = entry.exec(section); m; m = entry.exec(section)) {
      record(unescape(m[1]), from + m.index);
    }
  }
  return found;
}

/** JSON string escapes, which a host or domain can legitimately carry none of. */
function unescape(raw: string): string {
  try {
    return JSON.parse(`"${raw}"`) as string;
  } catch {
    return raw;
  }
}
