// Package output renders ranked items. Four formats: a ranked table (CLI), a
// Prometheus metrics endpoint, an iCal feed so renewals land in the team
// calendar, and JSON for CI.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fabiocicerchia/local-ai-lab/expiry-radar/internal/rank"
)

type Format string

const (
	FormatTable      Format = "table"
	FormatJSON       Format = "json"
	FormatICal       Format = "ical"       // renewals in the team calendar
	FormatPrometheus Format = "prometheus" // scrape target
)

// Formats lists every renderer, for CLI help and validation.
var Formats = []Format{FormatTable, FormatJSON, FormatICal, FormatPrometheus}

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
	default:
		return fmt.Errorf("unknown format %q (want one of %v)", format, Formats)
	}
}

func renderTable(w io.Writer, items []rank.Scored) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PRIORITY\tBLAST\tEXPIRES IN\tKIND\tNAME\tSOURCE\tWHY")
	for _, s := range items {
		fmt.Fprintf(tw, "%.2f\t%.2f\t%s\t%s\t%s\t%s\t%s\n",
			s.Priority, s.BlastRadius, humanDays(s.DaysLeft), s.Item.Kind, displayName(s), s.Item.Source, s.Why)
	}
	if len(items) == 0 {
		fmt.Fprintln(tw, "(nothing expiring — or no sources were enabled)")
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
