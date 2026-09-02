/**
 * Everything the extension asks the user, and nothing it does with the answer.
 *
 * The wording is the product here: what a kind of item *is*, and why the choice
 * matters, is the only thing standing between the operator and a config file
 * they have to learn the schema of. Keeping it apart from the commands means
 * the sequence of prompts can be read — and tested — in one place.
 */
import * as vscode from 'vscode';

import { EntryKind, invalidExpires, MANUAL_KINDS, renderEntry } from './edit';
import { Format } from './runner';

export interface ExportChoice extends vscode.QuickPickItem {
  format: Format;
  ext: string;
}

/** A selection is usually the thing being recorded — offer it as the default. */
export function selectedText(): string {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.selection.isEmpty) return '';
  return editor.document.getText(editor.selection).trim();
}

/**
 * Which kind of thing is being recorded.
 *
 * Two of the six kinds of item are recorded rather than discovered — a host to
 * probe and a domain to look up — and the third option is for what nothing can
 * find at all: a registrar with no RDAP, a credential rotated by hand, a
 * code-signing certificate on somebody's laptop.
 */
export async function pickEntryKind(): Promise<EntryKind | undefined> {
  const what = await vscode.window.showQuickPick(
    [
      {
        label: 'Endpoint',
        detail: 'A host to probe over TLS — its certificate and every intermediate in its chain.',
        entry: 'endpoint' as EntryKind,
      },
      {
        label: 'Domain',
        detail: 'A registration to check via RDAP.',
        entry: 'domain' as EntryKind,
      },
      {
        label: 'Something nothing can discover',
        detail:
          'A date you know: a registrar with no RDAP, a credential rotated by hand, a contract.',
        entry: 'manual' as EntryKind,
      },
    ],
    { title: 'expiry-radar: record what?', placeHolder: 'Everything else is discovered, not recorded' },
  );
  return what?.entry;
}

/** The prompts for one kind of entry, or undefined if the user backed out. */
export async function promptEntry(kind: EntryKind): Promise<string | undefined> {
  if (kind !== 'manual') {
    const isHost = kind === 'endpoint';
    const value = await vscode.window.showInputBox({
      title: isHost ? 'expiry-radar: record an endpoint' : 'expiry-radar: record a domain',
      prompt: isHost ? 'Host to probe over TLS. Port optional.' : 'Domain to check via RDAP.',
      placeHolder: isHost ? 'shop.example.com' : 'example.com',
      value: selectedText(),
      validateInput: (v) => (v.trim() ? undefined : 'A value is required.'),
    });
    return value?.trim() ? renderEntry(kind, value) : undefined;
  }
  return promptManualEntry();
}

/** Name, then kind, then date — the three things nothing can discover for you. */
async function promptManualEntry(): Promise<string | undefined> {
  const name = await vscode.window.showInputBox({
    title: 'expiry-radar: record an item — 1 of 3',
    prompt: 'What is it? This is the name the report will show.',
    placeHolder: 'acme-corp.co.uk',
    value: selectedText(),
    validateInput: (v) => (v.trim() ? undefined : 'A name is required.'),
  });
  if (!name?.trim()) return undefined;

  // The kind is not cosmetic: it picks the base blast radius, which is what
  // decides where this lands in the ranking.
  const kindPick = await vscode.window.showQuickPick(
    MANUAL_KINDS.map((k) => ({ label: k.label, detail: k.hint, itemKind: k.kind })),
    {
      title: 'expiry-radar: record an item — 2 of 3',
      placeHolder: 'What kind? This sets its base blast radius.',
    },
  );
  if (!kindPick) return undefined;

  const expires = await vscode.window.showInputBox({
    title: 'expiry-radar: record an item — 3 of 3',
    prompt: 'When does it expire? YYYY-MM-DD, or a full RFC 3339 timestamp.',
    placeHolder: '2027-03-01',
    validateInput: invalidExpires,
  });
  if (!expires?.trim()) return undefined;

  return renderEntry('manual', { name, kind: kindPick.itemKind, expires });
}

export function pickExportFormat(): Thenable<ExportChoice | undefined> {
  return vscode.window.showQuickPick<ExportChoice>(
    [
      { label: 'HTML report', description: 'self-contained, for mailing or publishing', format: 'html', ext: 'html' },
      { label: 'iCal feed', description: 'renewals as calendar events, with alarms by blast radius', format: 'ical', ext: 'ics' },
      { label: 'JSON', description: 'the ranked inventory, for CI', format: 'json', ext: 'json' },
      { label: 'Prometheus metrics', description: 'a scrape body', format: 'prometheus', ext: 'prom' },
    ],
    { title: 'expiry-radar: export as', placeHolder: 'Pick a format' },
  );
}

/** The host to probe, seeded from the selection. */
export function promptHost(): Thenable<string | undefined> {
  return vscode.window.showInputBox({
    title: 'expiry-radar: probe a host',
    prompt: 'Host to probe over TLS, and check as a domain. Port optional.',
    value: selectedText(),
    placeHolder: 'shop.example.com',
    validateInput: (value) => (value.trim() ? undefined : 'A host is required.'),
  });
}
