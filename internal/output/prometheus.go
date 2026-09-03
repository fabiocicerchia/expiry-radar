package output

import (
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
)

func renderPrometheus(w io.Writer, items []rank.Scored, opts Options) error {
	var b strings.Builder
	writeGauge(&b, items, "expiry_radar_seconds_left", "Seconds until this item expires (negative once expired).", func(s rank.Scored) string {
		return strconv.FormatInt(int64(s.Item.Expires.Sub(opts.Now).Seconds()), 10)
	})
	writeGauge(&b, items, "expiry_radar_blast_radius", "Inferred consequence of this item expiring, 0..1.", func(s rank.Scored) string {
		return strconv.FormatFloat(s.BlastRadius, 'f', 2, 64)
	})
	writeGauge(&b, items, "expiry_radar_priority", "Combined urgency and blast radius used for ordering, 0..1.", func(s rank.Scored) string {
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

// One HELP/TYPE header followed by one sample per item — the exposition format
// every gauge here shares.
func writeGauge(b *strings.Builder, items []rank.Scored, name, help string, value func(rank.Scored) string) {
	b.WriteString("# HELP " + name + " " + help + "\n# TYPE " + name + " gauge\n")
	for _, s := range items {
		b.WriteString(name + promLabels(s) + " " + value(s) + "\n")
	}
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
