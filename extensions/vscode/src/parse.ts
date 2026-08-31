/**
 * Turning one `-format json` report into the rows the panel, the status bar and
 * the diagnostics all read.
 *
 * Nothing here re-ranks: `internal/rank` already did that, and a second opinion
 * in the editor would be a different tool wearing the same name. The report
 * arrives in priority order and stays in it.
 */
import { Item, Kind, Report, ReportItem, Severity } from './types';

/** Worst deadline first — the panel's order and the filter's order. */
export const SEVERITIES: Severity[] = ['expired', 'urgent', 'soon', 'ok'];

export const SEVERITY_RANK: Record<Severity, number> = {
  expired: 0,
  urgent: 1,
  soon: 2,
  ok: 3,
};

export const SEVERITY_LABEL: Record<Severity, string> = {
  expired: 'Expired',
  urgent: 'Within 14 days',
  soon: 'Within 30 days',
  ok: 'Further out',
};

/** The kinds the CLI ships today, in the order `internal/rank` weights them. */
export const KINDS: Kind[] = [
  'domain',
  'intermediate_ca',
  'tls_cert',
  'iam_access_key',
  'secret',
  'vault_lease',
];

const KIND_LABELS: Record<string, string> = {
  tls_cert: 'TLS certificate',
  intermediate_ca: 'Intermediate CA',
  secret: 'Secret',
  iam_access_key: 'IAM access key',
  vault_lease: 'Vault lease',
  domain: 'Domain',
};

/** A kind the extension has never heard of still gets a readable label. */
export function kindLabel(kind: Kind): string {
  return KIND_LABELS[kind] ?? String(kind).replace(/_/g, ' ');
}

/**
 * Colour follows the deadline, not the priority. The default thresholds are the
 * HTML report's, so a row is the same colour in the panel and in the report.
 */
export function severity(daysLeft: number, warnWithin = 14, infoWithin = 30): Severity {
  if (daysLeft < 0) return 'expired';
  if (daysLeft <= warnWithin) return 'urgent';
  if (daysLeft <= infoWithin) return 'soon';
  return 'ok';
}

/** Whole days, and never a cheerful "0 days" for something already broken. */
export function humanDays(days: number): string {
  if (days < 0) return `expired ${Math.floor(-days)}d ago`;
  if (days < 1) return 'today';
  return `${Math.floor(days)}d`;
}

/** `internal/output.displayName` — the namespace is a prefix, not a repeat. */
export function displayName(item: ReportItem): string {
  const ns = item.namespace ?? '';
  if (ns && !item.name.startsWith(`${ns}/`)) return `${ns}/${item.name}`;
  return item.name;
}

/**
 * Stable across runs, so a row keeps its identity (and its expansion state)
 * when the inventory is refreshed. The same key the iCal UID is built from,
 * minus the date — an item whose expiry moved because somebody renewed it is
 * still the same item.
 */
export function itemId(item: ReportItem): string {
  return `${item.kind} ${item.source} ${displayName(item)}`;
}

export function toItems(report: Report, warnWithin?: number, infoWithin?: number): Item[] {
  const seen = new Set<string>();
  return (report.items ?? []).map((raw) => {
    // Two sources can legitimately report the same thing (a cert in Vault and
    // the same cert served by an endpoint); the tree needs distinct ids anyway.
    let id = itemId(raw);
    for (let n = 2; seen.has(id); n += 1) id = `${itemId(raw)} ${n}`;
    seen.add(id);
    return {
      ...raw,
      id,
      display: displayName(raw),
      severity: severity(raw.daysLeft, warnWithin, infoWithin),
    };
  });
}

/** Worst deadline first, then highest priority — the report's own tiebreak. */
export function compareItems(a: Item, b: Item): number {
  if (a.severity !== b.severity) return SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity];
  if (a.priority !== b.priority) return b.priority - a.priority;
  if (a.daysLeft !== b.daysLeft) return a.daysLeft - b.daysLeft;
  return a.display.localeCompare(b.display);
}

const WARNING_LINE = /^expiry-radar:\s*warning:\s*(.+)$/;

/**
 * The sources that failed, from stderr.
 *
 * The CLI prints one `expiry-radar: warning: <source>: <error>` line per failed
 * source and still reports everything the others managed to read — so a run can
 * succeed, look clean, and be missing an entire cloud account. These lines are
 * the only evidence of that, so they are shown, never merely logged.
 */
export function parseWarnings(stderr: string): string[] {
  const out: string[] = [];
  for (const line of stderr.split('\n')) {
    const match = WARNING_LINE.exec(line.trim());
    if (match) out.push(match[1].trim());
  }
  return out;
}

/** The facts behind a row, as the tooltip, the hover and the log all want them. */
export function describe(item: Item): string[] {
  const facts = [`kind: ${kindLabel(item.kind)}`, `source: ${item.source}`];
  if (item.namespace) facts.push(`namespace: ${item.namespace}`);
  facts.push(
    `expires: ${item.expires.slice(0, 10)} (${humanDays(item.daysLeft)})`,
    `priority: ${item.priority.toFixed(2)}`,
    `blast radius: ${item.blastRadius.toFixed(2)}`,
  );
  return facts;
}
