/**
 * The shape `expiry-radar -format json` writes, plus what the extension keeps
 * on top of it.
 *
 * The CLI's renderer is the contract — see `renderJSON` in
 * `internal/output/output.go`. Fields are read, never invented: anything the
 * extension wants that the report does not carry (a severity bucket, a place in
 * the config file) is derived here, in parse.ts and locate.ts, so the two never
 * disagree about what the CLI actually said.
 */

/** `source.Kind` — what expires. Open-ended: a new source may add one. */
export type Kind =
  | 'tls_cert'
  | 'intermediate_ca'
  | 'secret'
  | 'iam_access_key'
  | 'vault_lease'
  | 'domain'
  | (string & {});

/**
 * How close the deadline is, on the ladder the HTML report already uses. This
 * follows the deadline, never the priority: a panel you skim has to make
 * "already broken" and "broken next week" impossible to miss, and priority
 * deliberately floats a calm 100-day domain above an urgent staging cert.
 */
export type Severity = 'expired' | 'urgent' | 'soon' | 'ok';

export interface ReportItem {
  priority: number;
  blastRadius: number;
  daysLeft: number;
  expired: boolean;
  kind: Kind;
  name: string;
  namespace?: string;
  source: string;
  /** RFC 3339, as Go's time.Time marshals it. */
  expires: string;
  why: string;
  labels?: Record<string, string>;
}

export interface Report {
  generatedAt: string;
  count: number;
  expired: number;
  items: ReportItem[];
}

/** A report item with everything the editor needs to place and paint it. */
export interface Item extends ReportItem {
  /** Stable across runs: used to address a row and to dedupe. */
  id: string;
  /** `namespace/name` when the namespace is not already part of the name. */
  display: string;
  severity: Severity;
  /** Where in the config file this item was declared, when it was declared. */
  origin?: Origin;
}

/** A line in the config file that declares an item the report came back with. */
export interface Origin {
  file: string;
  /** 1-based, as the config file is read. */
  line: number;
  column: number;
}

/** One completed collection against one workspace folder. */
export interface Snapshot {
  items: Item[];
  /**
   * Sources that failed, verbatim from stderr. Never merely logged: a report
   * that quietly lost a source reads exactly like a clean estate, which is the
   * one failure mode this tool exists to prevent.
   */
  warnings: string[];
  generatedAt: string;
  /** The config file this run read, when it read one. */
  configPath: string;
  at: number;
  durationMs: number;
}
