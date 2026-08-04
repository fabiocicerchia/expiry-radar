// Package output renders ranked items. Five formats: a ranked table (CLI), a
// Prometheus metrics endpoint, an iCal feed so renewals land in the team
// calendar, JSON for CI, and a self-contained HTML report.
package output

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

type Format string

const (
	FormatTable      Format = "table"
	FormatJSON       Format = "json"
	FormatICal       Format = "ical"       // renewals in the team calendar
	FormatPrometheus Format = "prometheus" // scrape target
	FormatHTML       Format = "html"       // a self-contained report to email or publish
)

// Formats lists every renderer, for CLI help and validation.
var Formats = []Format{FormatTable, FormatJSON, FormatICal, FormatPrometheus, FormatHTML}

// Options exists so the renderers are testable without freezing the clock
// globally — an iCal feed with a moving DTSTAMP cannot be diffed.
type Options struct {
	Now time.Time
}

// Render writes the scored items in the requested format.
func Render(w io.Writer, items []rank.Scored, format Format) error {
	return RenderAt(w, items, format, Options{Now: time.Now()})
}

func RenderAt(w io.Writer, items []rank.Scored, format Format, opts Options) error {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	switch format {
	case FormatTable:
		return renderTable(w, items)
	case FormatJSON:
		return renderJSON(w, items, opts)
	case FormatICal:
		return renderICal(w, items, opts)
	case FormatPrometheus:
		return renderPrometheus(w, items, opts)
	case FormatHTML:
		return renderHTML(w, items, opts)
	default:
		return fmt.Errorf("unknown format %q (want one of %v)", format, Formats)
	}
}

func renderTable(w io.Writer, items []rank.Scored) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// tabwriter buffers until Flush, which is what actually reports write errors.
	_, _ = fmt.Fprintln(tw, "PRIORITY\tBLAST\tEXPIRES IN\tKIND\tNAME\tSOURCE\tWHY")
	for _, s := range items {
		_, _ = fmt.Fprintf(tw, "%.2f\t%.2f\t%s\t%s\t%s\t%s\t%s\n",
			s.Priority, s.BlastRadius, humanDays(s.DaysLeft), s.Item.Kind, displayName(s), s.Item.Source, s.Why)
	}
	if len(items) == 0 {
		_, _ = fmt.Fprintln(tw, "(nothing expiring — or no sources were enabled)")
	}
	return tw.Flush()
}

func displayName(s rank.Scored) string {
	if s.Item.Namespace != "" && !strings.HasPrefix(s.Item.Name, s.Item.Namespace+"/") {
		return s.Item.Namespace + "/" + s.Item.Name
	}
	return s.Item.Name
}

// Whole days, and never a cheerful "0 days" for something already broken.
func humanDays(days float64) string {
	switch {
	case days < 0:
		return fmt.Sprintf("EXPIRED %dd ago", int(-days))
	case days < 1:
		return "today"
	default:
		return fmt.Sprintf("%dd", int(days))
	}
}

type jsonItem struct {
	Priority    float64           `json:"priority"`
	BlastRadius float64           `json:"blastRadius"`
	DaysLeft    float64           `json:"daysLeft"`
	Expired     bool              `json:"expired"`
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Source      string            `json:"source"`
	Expires     time.Time         `json:"expires"`
	Why         string            `json:"why"`
	Labels      map[string]string `json:"labels,omitempty"`
}

func renderJSON(w io.Writer, items []rank.Scored, opts Options) error {
	out := struct {
		GeneratedAt time.Time  `json:"generatedAt"`
		Count       int        `json:"count"`
		Expired     int        `json:"expired"`
		Items       []jsonItem `json:"items"`
	}{GeneratedAt: opts.Now.UTC(), Count: len(items), Items: make([]jsonItem, 0, len(items))}

	for _, s := range items {
		if s.DaysLeft < 0 {
			out.Expired++
		}
		out.Items = append(out.Items, jsonItem{
			Priority:    s.Priority,
			BlastRadius: s.BlastRadius,
			DaysLeft:    math.Round(s.DaysLeft*100) / 100,
			Expired:     s.DaysLeft < 0,
			Kind:        string(s.Item.Kind),
			Name:        s.Item.Name,
			Namespace:   s.Item.Namespace,
			Source:      s.Item.Source,
			Expires:     s.Item.Expires.UTC(),
			Why:         s.Why,
			Labels:      s.Item.Labels,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// --- iCal (RFC 5545) ---

// Renewals land in the team calendar as all-day events. Higher blast radius gets
// an earlier alarm, because that is the whole point of ranking.
func renderICal(w io.Writer, items []rank.Scored, opts Options) error {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//expiry-radar//EN\r\nCALSCALE:GREGORIAN\r\nMETHOD:PUBLISH\r\nX-WR-CALNAME:Expiry radar\r\n")
	stamp := opts.Now.UTC().Format("20060102T150405Z")

	for _, s := range items {
		day := s.Item.Expires.UTC()
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString(fold("UID:" + uid(s)))
		b.WriteString(fold("DTSTAMP:" + stamp))
		b.WriteString(fold("DTSTART;VALUE=DATE:" + day.Format("20060102")))
		b.WriteString(fold("DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format("20060102")))
		b.WriteString(fold(fmt.Sprintf("SUMMARY:%s expires: %s", s.Item.Kind, escapeText(displayName(s)))))
		b.WriteString(fold(fmt.Sprintf("DESCRIPTION:blast radius %.2f (%s)\\nsource %s\\npriority %.2f",
			s.BlastRadius, escapeText(s.Why), escapeText(s.Item.Source), s.Priority)))
		b.WriteString(fold("CATEGORIES:" + strings.ToUpper(string(s.Item.Kind))))
		b.WriteString(alarm(s))
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func alarm(s rank.Scored) string {
	lead := "-P7D"
	switch {
	case s.BlastRadius >= 0.8:
		lead = "-P30D"
	case s.BlastRadius >= 0.6:
		lead = "-P14D"
	}
	return "BEGIN:VALARM\r\nACTION:DISPLAY\r\n" + fold("DESCRIPTION:Renew "+escapeText(displayName(s))) + "TRIGGER:" + lead + "\r\nEND:VALARM\r\n"
}

// Stable across runs so calendars update the same event instead of piling up
// duplicates every time the feed is refreshed.
func uid(s rank.Scored) string {
	key := string(s.Item.Kind) + "-" + s.Item.Source + "-" + displayName(s) + "-" + s.Item.Expires.UTC().Format("20060102")
	return sanitiseUID(key) + "@expiry-radar"
}

func sanitiseUID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func escapeText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(s)
}

// RFC 5545 caps content lines at 75 octets; long certificate names blow straight
// past that and some clients do reject it.
func fold(line string) string {
	const limit = 73
	var b strings.Builder
	for len(line) > limit {
		b.WriteString(line[:limit])
		b.WriteString("\r\n ")
		line = line[limit:]
	}
	b.WriteString(line)
	b.WriteString("\r\n")
	return b.String()
}

// --- Prometheus ---

func renderPrometheus(w io.Writer, items []rank.Scored, opts Options) error {
	var b strings.Builder
	gauge := func(name, help string, value func(rank.Scored) string) {
		b.WriteString("# HELP " + name + " " + help + "\n# TYPE " + name + " gauge\n")
		for _, s := range items {
			b.WriteString(name + promLabels(s) + " " + value(s) + "\n")
		}
	}
	gauge("expiry_radar_seconds_left", "Seconds until this item expires (negative once expired).", func(s rank.Scored) string {
		return strconv.FormatInt(int64(s.Item.Expires.Sub(opts.Now).Seconds()), 10)
	})
	gauge("expiry_radar_blast_radius", "Inferred consequence of this item expiring, 0..1.", func(s rank.Scored) string {
		return strconv.FormatFloat(s.BlastRadius, 'f', 2, 64)
	})
	gauge("expiry_radar_priority", "Combined urgency and blast radius used for ordering, 0..1.", func(s rank.Scored) string {
		return strconv.FormatFloat(s.Priority, 'f', 2, 64)
	})

	b.WriteString("# HELP expiry_radar_items Total items inventoried, by kind.\n# TYPE expiry_radar_items gauge\n")
	counts := countByKind(items)
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		b.WriteString("expiry_radar_items{kind=\"" + escapeLabel(k) + "\"} " + strconv.Itoa(counts[k]) + "\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func countByKind(items []rank.Scored) map[string]int {
	counts := map[string]int{}
	for _, s := range items {
		counts[string(s.Item.Kind)]++
	}
	return counts
}

// Only the low-cardinality fields become labels. Certificate serials and host
// lists do not belong in a time series.
func promLabels(s rank.Scored) string {
	pairs := [][2]string{
		{"kind", string(s.Item.Kind)},
		{"name", s.Item.Name},
		{"namespace", s.Item.Namespace},
		{"source", s.Item.Source},
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		parts = append(parts, p[0]+"=\""+escapeLabel(p[1])+"\"")
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

// --- HTML ---

// One self-contained file: no external CSS, fonts or scripts, so it survives
// being mailed as an attachment or dropped on a static host. html/template
// escapes the values, which matters because names come from certificates and
// registries — untrusted text.
func renderHTML(w io.Writer, items []rank.Scored, opts Options) error {
	groups, stats := groupRows(items)
	kinds := map[string]bool{}
	for _, s := range items {
		kinds[string(s.Item.Kind)] = true
	}
	return htmlReport.Execute(w, struct {
		Generated string
		Groups    []htmlGroup
		Kinds     []string
		Stats     htmlStats
	}{opts.Now.UTC().Format("2006-01-02 15:04 UTC"), groups, sortedKeys(kinds), stats})
}

type htmlRow struct {
	rank.Scored
	Name, Left, On, Sev, Percent string
}

type htmlGroup struct {
	Name    string
	Rows    []htmlRow
	Soonest string // the deadline that actually matters for this group
	Sev     string
}

type htmlStats struct {
	Total, Expired, In14, In30 int
	NextName, NextIn, NextSev  string
}

// Colour follows the deadline, not the priority: a report you skim must make
// "already broken" and "broken next week" impossible to miss. Every colour is
// paired with the day count in text, so it never carries the meaning alone.
func severity(days float64) string {
	switch {
	case days < 0:
		return "expired"
	case days <= 14:
		return "urgent"
	case days <= 30:
		return "soon"
	default:
		return "ok"
	}
}

// Rows are grouped under the domains the report already knows about, which
// avoids guessing where a registrable name ends — "co.uk" is not a domain and no
// public-suffix list is worth a dependency here. Anything that belongs to no
// tracked domain falls back to its namespace, then to a shared bucket.
func groupRows(items []rank.Scored) ([]htmlGroup, htmlStats) {
	var domains []string
	for _, s := range items {
		if s.Item.Kind == source.KindDomain {
			domains = append(domains, s.Item.Name)
		}
	}
	// Longest first, so a.b.example.com lands under the more specific of two
	// tracked domains rather than whichever was collected first.
	sort.Slice(domains, func(i, j int) bool { return len(domains[i]) > len(domains[j]) })

	var stats htmlStats
	byName := map[string]*htmlGroup{}
	var order []string
	for _, s := range items {
		stats.Total++
		switch {
		case s.DaysLeft < 0:
			stats.Expired++
		case s.DaysLeft <= 14:
			stats.In14++
			fallthrough
		case s.DaysLeft <= 30:
			stats.In30++
		}
		key := groupOf(s, domains)
		g, ok := byName[key]
		if !ok {
			g = &htmlGroup{Name: key}
			byName[key] = g
			order = append(order, key)
		}
		g.Rows = append(g.Rows, htmlRow{
			Scored:  s,
			Name:    displayName(s),
			Left:    humanDays(s.DaysLeft),
			On:      s.Item.Expires.UTC().Format("2006-01-02"),
			Sev:     severity(s.DaysLeft),
			Percent: fmt.Sprintf("%.0f", s.Priority*100),
		})
	}

	out := make([]htmlGroup, 0, len(order))
	soonest := math.Inf(1)
	for _, k := range order {
		g := byName[k]
		worst := math.Inf(1)
		for _, r := range g.Rows {
			if r.DaysLeft < worst {
				worst = r.DaysLeft
			}
			if r.DaysLeft < soonest {
				soonest, stats.NextName, stats.NextIn, stats.NextSev = r.DaysLeft, r.Name, r.Left, r.Sev
			}
		}
		g.Soonest, g.Sev = humanDays(worst), severity(worst)
		out = append(out, *g)
	}
	// Groups already arrive in ranked order (items are sorted by priority), so
	// the first group holds the highest-priority item. Keep that.
	return out, stats
}

func groupOf(s rank.Scored, domains []string) string {
	name := s.Item.Name
	for _, d := range domains {
		if name == d || strings.HasSuffix(name, "."+d) {
			return d
		}
	}
	if s.Item.Namespace != "" {
		return s.Item.Namespace
	}
	if s.Item.Kind == source.KindIntermediate {
		return "shared chain"
	}
	return "other"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var htmlReport = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>expiry-radar report</title>
<style>
:root{color-scheme:light dark;
--bg:#fcfcfb;--card:#fff;--fg:#1a1a19;--mute:#6b6b68;--line:#e4e4e1;--head:#f4f4f2;
--expired:#d03b3b;--urgent:#ec835a;--soon:#fab219;--ok:#0ca30c}
@media(prefers-color-scheme:dark){:root{--bg:#1a1a19;--card:#212120;--fg:#e9e9e6;--mute:#9a9a95;--line:#32322f;--head:#262625}}
*{box-sizing:border-box}
body{background:var(--bg);color:var(--fg);font:15px/1.55 system-ui,-apple-system,sans-serif;margin:0;padding:2rem 1rem 4rem}
main{max-width:74rem;margin:0 auto}
header h1{font-size:1.5rem;margin:0 0 .2rem;letter-spacing:-.01em}
header p{color:var(--mute);margin:0 0 1.75rem;font-size:.875rem}
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(9.5rem,1fr));gap:.75rem;margin-bottom:1.5rem}
.stat{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:.85rem 1rem}
.stat .k{font-size:.7rem;text-transform:uppercase;letter-spacing:.06em;color:var(--mute);display:block;margin-bottom:.3rem}
.stat .v{font-size:1.75rem;font-weight:650;line-height:1.1;letter-spacing:-.02em;display:block}
.stat .s{font-size:.8rem;color:var(--mute);display:block;margin-top:.15rem;overflow:hidden;text-overflow:ellipsis}
.stat.next .v{font-size:1.15rem;word-break:break-all}
.dot{display:inline-block;width:.55rem;height:.55rem;border-radius:50%;margin-right:.4rem;vertical-align:baseline}
.expired .dot,.stat.expired .v{color:var(--expired)}
.dot.expired{background:var(--expired)}.dot.urgent{background:var(--urgent)}
.dot.soon{background:var(--soon)}.dot.ok{background:var(--ok)}
.controls{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;margin-bottom:1.25rem}
input[type=search]{flex:1 1 14rem;min-width:10rem;padding:.45rem .7rem;font:inherit;font-size:.9rem;
background:var(--card);color:var(--fg);border:1px solid var(--line);border-radius:7px}
.chip{padding:.35rem .7rem;font-size:.82rem;background:var(--card);color:var(--mute);
border:1px solid var(--line);border-radius:99px;cursor:pointer}
.chip[aria-pressed=true]{background:var(--fg);color:var(--bg);border-color:var(--fg)}
.count{font-size:.82rem;color:var(--mute);margin-left:auto}
.group{background:var(--card);border:1px solid var(--line);border-radius:10px;margin-bottom:1rem;overflow:hidden}
.group>summary{padding:.7rem 1rem;cursor:pointer;font-weight:600;display:flex;gap:.6rem;align-items:baseline}
.group>summary::-webkit-details-marker{display:none}
.group>summary::before{content:"▸";color:var(--mute);font-size:.8rem;transition:transform .12s}
.group[open]>summary::before{content:"▾"}
.group .n{font-size:.8rem;font-weight:400;color:var(--mute);margin-left:auto}
.scroll{overflow-x:auto;border-top:1px solid var(--line)}
/* Fixed layout with shared widths, so columns line up across every group table
   rather than each one sizing to its own contents. */
table{border-collapse:collapse;width:100%;min-width:56rem;table-layout:fixed;font-size:.88rem}
th,td{padding:.5rem .75rem;text-align:left;border-bottom:1px solid var(--line);white-space:nowrap;
overflow:hidden;text-overflow:ellipsis}
td.name{white-space:normal;word-break:break-word}
th{background:var(--head);font-weight:600;font-size:.72rem;letter-spacing:.05em;text-transform:uppercase;color:var(--mute)}
tr:last-child td{border-bottom:0}
td.why{white-space:normal;overflow:visible;color:var(--mute);font-size:.84rem}
td.left{font-variant-numeric:tabular-nums}
tr.expired td.left{color:var(--expired);font-weight:650}
tr.urgent td.left{font-weight:650}
td.pri{font-variant-numeric:tabular-nums}
.bar{display:inline-block;vertical-align:middle;width:2.6rem;height:.35rem;border-radius:2px;background:var(--line);margin-right:.5rem}
.bar>span{display:block;height:100%;border-radius:2px;background:var(--mute)}
.empty{padding:2.5rem 1rem;text-align:center;color:var(--mute)}
[hidden]{display:none!important}
</style></head><body><main>
<header>
<h1>expiry-radar</h1>
<p>generated {{.Generated}}</p>
</header>

<section class="stats">
<div class="stat"><span class="k">Tracked</span><span class="v">{{.Stats.Total}}</span><span class="s">items</span></div>
<div class="stat{{if .Stats.Expired}} expired{{end}}"><span class="k">Expired</span><span class="v">{{.Stats.Expired}}</span><span class="s">already broken</span></div>
<div class="stat"><span class="k">Next 14 days</span><span class="v">{{.Stats.In14}}</span><span class="s">need action now</span></div>
<div class="stat"><span class="k">Next 30 days</span><span class="v">{{.Stats.In30}}</span><span class="s">on the horizon</span></div>
{{- if .Stats.NextName}}
<div class="stat next"><span class="k">Soonest</span><span class="v">{{.Stats.NextName}}</span><span class="s"><i class="dot {{.Stats.NextSev}}"></i>{{.Stats.NextIn}}</span></div>
{{- end}}
</section>

<div class="controls">
<input type="search" id="q" placeholder="Filter by name, source or reason…" aria-label="Filter rows">
<button class="chip" data-kind="" aria-pressed="true">all kinds</button>
{{- range .Kinds}}
<button class="chip" data-kind="{{.}}" aria-pressed="false">{{.}}</button>
{{- end}}
<button class="chip" data-sev="soon30" aria-pressed="false">≤ 30 days</button>
<span class="count" id="count"></span>
</div>

{{- if .Groups}}
{{- range .Groups}}
<details class="group" data-group open>
<summary><i class="dot {{.Sev}}"></i>{{.Name}}<span class="n"><span data-visible>{{len .Rows}}</span> of {{len .Rows}} · soonest {{.Soonest}}</span></summary>
<div class="scroll">
<table>
<colgroup><col style="width:7.5rem"><col style="width:4.5rem"><col style="width:7.5rem"><col style="width:7rem"><col style="width:8.5rem"><col style="width:16rem"><col style="width:8.5rem"><col></colgroup>
<thead><tr><th>Priority</th><th>Blast</th><th>Expires in</th><th>On</th><th>Kind</th><th>Name</th><th>Source</th><th>Why</th></tr></thead>
<tbody>
{{- range .Rows}}
<tr class="{{.Sev}}" data-kind="{{.Item.Kind}}" data-sev="{{.Sev}}" data-days="{{printf "%.0f" .DaysLeft}}" data-text="{{.Name}} {{.Item.Source}} {{.Item.Kind}} {{.Why}}">
<td class="pri"><span class="bar"><span style="width:{{.Percent}}%"></span></span>{{printf "%.2f" .Priority}}</td>
<td>{{printf "%.2f" .BlastRadius}}</td>
<td class="left">{{.Left}}</td>
<td>{{.On}}</td>
<td>{{.Item.Kind}}</td>
<td class="name">{{.Name}}</td>
<td>{{.Item.Source}}</td>
<td class="why">{{.Why}}</td>
</tr>
{{- end}}
</tbody></table>
</div>
</details>
{{- end}}
{{- else}}
<p class="empty">Nothing expiring — or no sources were enabled.</p>
{{- end}}

<script>
(function () {
  var q = document.getElementById('q'), count = document.getElementById('count');
  var chips = [].slice.call(document.querySelectorAll('.chip'));
  var kind = '', soon = false;

  function apply() {
    var needle = q.value.toLowerCase(), shown = 0, total = 0;
    [].forEach.call(document.querySelectorAll('[data-group]'), function (g) {
      var visible = 0, rows = g.querySelectorAll('tbody tr');
      [].forEach.call(rows, function (tr) {
        var ok = (!kind || tr.dataset.kind === kind) &&
                 (!soon || +tr.dataset.days <= 30) &&
                 (!needle || tr.dataset.text.toLowerCase().indexOf(needle) > -1);
        tr.hidden = !ok;
        if (ok) visible++;
      });
      g.querySelector('[data-visible]').textContent = visible;
      g.hidden = visible === 0;
      shown += visible; total += rows.length;
    });
    count.textContent = shown === total ? total + ' items' : shown + ' of ' + total + ' items';
  }

  chips.forEach(function (c) {
    c.addEventListener('click', function () {
      if ('sev' in c.dataset) {
        soon = !soon;
        c.setAttribute('aria-pressed', soon);
      } else {
        kind = c.dataset.kind;
        chips.forEach(function (o) {
          if (!('sev' in o.dataset)) o.setAttribute('aria-pressed', o === c);
        });
      }
      apply();
    });
  });
  q.addEventListener('input', apply);
  apply();
})();
</script>
</main></body></html>
`))
