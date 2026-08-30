/**
 * The report, shown exactly as expiry-radar renders it.
 *
 * The CLI already writes a self-contained HTML report — inline CSS and JS, no
 * network references, because it is meant to survive being mailed as an
 * attachment — so the extension shows that document rather than reimplementing
 * the layout. The report in the editor and the report in CI are literally the
 * same bytes, filters and all.
 *
 * Two small adaptations: a CSP, since a webview needs one, and a bridge that
 * hands external links to the editor. The light/dark palette needs nothing: the
 * report switches on `prefers-color-scheme`, which the editor propagates to a
 * webview from the active theme.
 */
import * as vscode from 'vscode';

const BRIDGE = `
(function(){
  var api = acquireVsCodeApi();
  document.addEventListener('click', function(e){
    var a = e.target && e.target.closest && e.target.closest('a[href^="http"]');
    if(!a) return;
    e.preventDefault();
    api.postMessage({type:'open', href: a.getAttribute('href')});
  });
})();
`;

function adapt(html: string, webview: vscode.Webview): string {
  // The report's own <style>/<script> are inline and unversioned, so
  // 'unsafe-inline' is unavoidable; everything else stays denied.
  const csp =
    `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; ` +
    `style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src ${webview.cspSource} data:;">`;
  return html
    .replace('<meta charset="utf-8">', `<meta charset="utf-8">${csp}`)
    .replace('</body>', `<script>${BRIDGE}</script></body>`);
}

export class ReportView {
  private panel?: vscode.WebviewPanel;
  private html = '';
  /** A newer report landed while the tab was hidden; it repaints on return. */
  private stale = false;

  /** The most recent report, so "Open Report" works without collecting again. */
  get current(): string {
    return this.html;
  }

  set current(html: string) {
    this.html = html;
    if (!html || !this.panel) return;
    // `retainContextWhenHidden` keeps the tab alive, so repainting a document
    // nobody is looking at would be pure cost on every background refresh.
    if (this.panel.visible) this.paint();
    else this.stale = true;
  }

  show(html: string, title: string): void {
    this.html = html;
    if (!this.panel) {
      this.panel = vscode.window.createWebviewPanel(
        'expiryRadar.report',
        'expiry-radar report',
        { viewColumn: vscode.ViewColumn.Active, preserveFocus: false },
        { enableScripts: true, enableFindWidget: true, retainContextWhenHidden: true },
      );
      this.panel.onDidDispose(() => (this.panel = undefined));
      this.panel.onDidChangeViewState(() => {
        if (this.panel?.visible && this.stale) this.paint();
      });
      this.panel.webview.onDidReceiveMessage((msg: { type: string; href?: string }) => {
        if (msg.type === 'open' && msg.href) {
          void vscode.env.openExternal(vscode.Uri.parse(msg.href));
        }
      });
    }
    this.panel.title = `expiry-radar — ${title}`;
    this.paint();
    this.panel.reveal(this.panel.viewColumn, false);
  }

  private paint(): void {
    if (!this.panel || !this.html) return;
    this.stale = false;
    this.panel.webview.html = adapt(this.html, this.panel.webview);
  }

  dispose(): void {
    this.panel?.dispose();
  }
}
