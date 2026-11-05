/**
 * What an entry in the config file is: the kinds that can be recorded, the
 * expiry the CLI will accept, and the JSON each one is written as. Putting it
 * into the file is splice.ts's job.
 */
import { Kind } from "./types";

/** What can be recorded, as opposed to discovered. */
export type EntryKind = "endpoint" | "domain" | "manual";

export interface ManualEntry {
  name: string;
  kind: Kind;
  /** RFC 3339, or YYYY-MM-DD — whatever `source.ManualItem` accepts. */
  expires: string;
  namespace?: string;
}

/** The config key each kind of entry lives under. */
export const ARRAY_FOR: Record<EntryKind, string> = {
  endpoint: "endpoints",
  domain: "domains",
  manual: "manual",
};

/** The CLI's `source.Kinds`, in the order `internal/rank` weights them. */
export const MANUAL_KINDS: { kind: Kind; label: string; hint: string }[] = [
  {
    kind: "trust_anchor",
    label: "Trust anchor",
    hint: "a root everything else validates against — nothing behind it fails gracefully",
  },
  { kind: "domain", label: "Domain", hint: "a registration — the whole estate, including mail" },
  { kind: "intermediate_ca", label: "Intermediate CA", hint: "every leaf it signed, at once" },
  { kind: "tls_cert", label: "TLS certificate", hint: "a code-signing or client cert, say" },
  { kind: "iam_access_key", label: "IAM access key", hint: "a key rotated by hand" },
  { kind: "secret", label: "Secret", hint: "an API token, a password" },
  { kind: "vault_lease", label: "Vault lease", hint: "a lease nothing enumerates" },
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
  if (!trimmed) return "A date is required.";
  const parts = DATE_ONLY.exec(trimmed) ?? RFC_3339.exec(trimmed);
  if (!parts) return "Use YYYY-MM-DD, or a full RFC 3339 timestamp.";
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
  if (kind === "domain") return JSON.stringify(String(value).trim());
  if (kind === "endpoint") return JSON.stringify({ host: String(value).trim() });
  const entry = value as ManualEntry;
  const out: Record<string, string> = {
    name: entry.name.trim(),
    kind: String(entry.kind),
    expires: entry.expires.trim(),
  };
  if (entry.namespace?.trim()) out.namespace = entry.namespace.trim();
  return JSON.stringify(out);
}

/** Which config array an item's source records into, or undefined if discovered. */
export function arrayForSource(source: string): string | undefined {
  if (source === "tls:endpoint") return "endpoints";
  if (source === "domain:rdap" || source === "domain:whois") return "domains";
  if (source === "manual") return "manual";
  return undefined;
}
