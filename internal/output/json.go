package output

import (
	"encoding/json"
	"io"
	"math"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
)

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

// jsonReport is the -format json contract, not an implementation detail: both
// editor integrations parse these field names (extensions/vscode/src/parse.ts
// and runner.ts, extensions/nvim/lua/expiry-radar/init.lua).
type jsonReport struct {
	GeneratedAt time.Time  `json:"generatedAt"`
	Count       int        `json:"count"`
	Expired     int        `json:"expired"`
	Items       []jsonItem `json:"items"`
}

func renderJSON(w io.Writer, items []rank.Scored, opts Options) error {
	out := jsonReport{GeneratedAt: opts.Now.UTC(), Count: len(items), Items: make([]jsonItem, 0, len(items))}

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
