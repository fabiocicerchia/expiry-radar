package output

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

// The report's markup lives beside this file rather than inside a Go string:
// it is 150 lines of HTML, CSS and JavaScript, and a raw literal hides all of
// it from every tool that could check it.
//
//go:embed report.html.tmpl
var reportHTML string

var htmlReport = template.Must(template.New("report").Parse(reportHTML))

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
	}{opts.Now.UTC().Format(generatedAt), groups, sortedKeys(kinds), stats})
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
	domains := trackedDomains(items)
	stats := countHorizons(items)

	byName := map[string]*htmlGroup{}
	var order []string
	for _, s := range items {
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
			On:      s.Item.Expires.UTC().Format(isoDate),
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

// The domains the report itself tracks, longest first so a.b.example.com lands
// under the more specific of two rather than whichever was collected first.
func trackedDomains(items []rank.Scored) []string {
	var domains []string
	for _, s := range items {
		if s.Item.Kind == source.KindDomain {
			domains = append(domains, s.Item.Name)
		}
	}
	sort.Slice(domains, func(i, j int) bool { return len(domains[i]) > len(domains[j]) })
	return domains
}

// The three deadlines the report leads with. The fallthrough is deliberate:
// anything due inside 14 days is also due inside 30.
func countHorizons(items []rank.Scored) htmlStats {
	var stats htmlStats
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
	}
	return stats
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
