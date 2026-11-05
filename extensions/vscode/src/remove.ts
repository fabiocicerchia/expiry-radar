/**
 * Removing an entry from the config file.
 *
 * Text surgery, for the reason edit.ts gives for adding: a round-trip through
 * the parser rewrites the whole document to delete one line of it.
 */
import { arraySpan } from "./locate";

/** Which config array an item's source records into, or undefined if discovered. */
export function arrayForSource(source: string): string | undefined {
  if (source === "tls:endpoint") return "endpoints";
  if (source === "domain:rdap" || source === "domain:whois") return "domains";
  if (source === "manual") return "manual";
  return undefined;
}

/** The offset of a 1-based line and column. */
function offsetOf(text: string, line: number, column: number): number {
  const lines = text.split("\n");
  if (line < 1 || line > lines.length) return -1;
  return lines.slice(0, line - 1).reduce((n, l) => n + l.length + 1, 0) + (column - 1);
}

/** The `[start, end)` of each element of an array, at its own depth only. */
function elementSpans(text: string, [open, close]: [number, number]): [number, number][] {
  const spans: [number, number][] = [];
  let depth = 0;
  let inString = false;
  let start = -1;
  for (let i = open + 1; i < close - 1; i += 1) {
    const ch = text[i];
    if (inString) {
      if (ch === "\\") i += 1;
      else if (ch === '"') inString = false;
      continue;
    }
    if (depth === 0 && start < 0 && !/\s/.test(ch) && ch !== ",") start = i;
    if (ch === '"') inString = true;
    else if (ch === "[" || ch === "{") depth += 1;
    else if (ch === "]" || ch === "}") depth -= 1;
    else if (ch === "," && depth === 0 && start >= 0) {
      spans.push([start, i]);
      start = -1;
    }
  }
  if (start >= 0) spans.push([start, close - 1]);
  // Trailing whitespace belongs to the layout, not the element.
  return spans.map(([a, b]): [number, number] => [a, a + text.slice(a, b).replace(/\s+$/, "").length]);
}

/**
 * Remove the entry recorded at `line`/`column` from the array under `key`.
 *
 * Bounded to that array by construction: the element is picked from the array's
 * own elements rather than by balancing brackets out from a line. Addressing by
 * line alone looked simpler and would have deleted the entire config on a
 * one-line file, where line 1 begins with the document's own opening brace.
 *
 * Returns undefined when the position names no element, which is the right
 * answer when the file has been edited since the collection that reported it.
 */
export function removeEntry(text: string, key: string, line: number, column: number): string | undefined {
  const span = arraySpan(text, key);
  if (!span) return undefined;
  const offset = offsetOf(text, line, column);
  if (offset < 0 || offset < span[0] || offset > span[1]) return undefined;

  const element = elementSpans(text, span).find(([a, b]) => offset >= a && offset <= b);
  if (!element) return undefined;

  let [from, to] = element;
  // Swallow the separator: the comma after it, or the one before it when this
  // was the last element. Leaving either behind produces invalid JSON.
  const after = /^\s*,/.exec(text.slice(to));
  if (after) {
    to += after[0].length;
    // And the rest of the line, so no blank line is left behind.
    to += (/^[ \t]*\n?/.exec(text.slice(to))?.[0] ?? "").length;
  } else {
    const before = /,\s*$/.exec(text.slice(0, from));
    if (before) from -= before[0].length;
  }
  const indent = /[ \t]*$/.exec(text.slice(0, from));
  if (indent) from -= indent[0].length;

  return text.slice(0, from) + text.slice(to);
}
