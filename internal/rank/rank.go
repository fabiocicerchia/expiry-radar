// Package rank orders expiring items by blast radius. This ranking IS the
// product — any script can list expiry dates; the value is ordering by
// consequence, so a cert on the payment path outranks one on a staging
// dashboard. Blast radius is inferred from traffic, ingress class and namespace,
// and must be overridable.
package rank

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

// Scored pairs an item with its computed urgency.
type Scored struct {
	Item        source.Item
	DaysLeft    float64
	BlastRadius float64 // 0..1, inferred consequence of this thing expiring
	Priority    float64 // combined urgency used for ordering
	Why         string  // what moved the blast radius — a ranking nobody can explain gets ignored
}

// Override lets an operator pin blast radius for specific names/namespaces.
type Override struct {
	Match       string  `json:"match"` // glob, matched against name, namespace and namespace/name
	BlastRadius float64 `json:"blastRadius"`
}

// Horizon is the window over which urgency ramps from 0 to 1. Ninety days is
// the usual "we could still renew this calmly" distance for a TLS certificate.
const Horizon = 90 * 24 * time.Hour

// Priority is a weighted sum, not a product. A product would rank everything
// beyond the horizon at exactly zero, throwing away the ordering that makes this
// tool worth running; the sum keeps a critical cert 100 days out above a staging
// cert 40 days out, while still floating anything imminent to the top.
const (
	weightUrgency = 0.55
	weightBlast   = 0.45
)

// Base blast radius per kind, before any label evidence.
var baseByKind = map[source.Kind]float64{
	source.KindDomain:       0.85, // the whole estate, including mail
	source.KindIntermediate: 0.80, // every leaf it signed, at once
	source.KindTLSCert:      0.50,
	source.KindIAMKey:       0.50,
	source.KindSecret:       0.45,
	source.KindVaultLease:   0.40,
}

// Rank sorts items by priority (soonest × highest blast radius first).
func Rank(items []source.Item, overrides []Override, now time.Time) []Scored {
	out := make([]Scored, 0, len(items))
	for _, it := range items {
		blast, why := blastRadius(it, overrides)
		daysLeft := it.Expires.Sub(now).Hours() / 24
		out = append(out, Scored{
			Item:        it,
			DaysLeft:    daysLeft,
			BlastRadius: round2(blast),
			Priority:    round2(weightUrgency*urgency(daysLeft) + weightBlast*blast),
			Why:         why,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].DaysLeft < out[j].DaysLeft
	})
	return out
}

// urgency ramps linearly from 0 at the horizon to 1 at the expiry date, and
// stays at 1 once expired — an expired thing cannot get more urgent.
func urgency(daysLeft float64) float64 {
	horizonDays := Horizon.Hours() / 24
	if daysLeft <= 0 {
		return 1
	}
	if daysLeft >= horizonDays {
		return 0
	}
	return (horizonDays - daysLeft) / horizonDays
}

func blastRadius(it source.Item, overrides []Override) (float64, string) {
	// An operator override is the final word — inference exists because most
	// estates have no reliable labels, not because it knows better.
	if o, ok := matchOverride(it, overrides); ok {
		return clamp01(o.BlastRadius), "override " + o.Match
	}
	// A label on the resource itself is the next best evidence.
	if v, err := strconv.ParseFloat(it.Labels[source.LabelBlastRadius], 64); err == nil {
		return clamp01(v), "labelled on the resource"
	}

	score, ok := baseByKind[it.Kind]
	if !ok {
		score = 0.4
	}
	b := blastScore{score: score}

	if isPublic(it) {
		b.adjust(0.20, "internet-facing")
	} else if class := it.Labels[source.LabelIngressClass]; class != "" && isInternalClass(class) {
		b.adjust(-0.15, "internal ingress class")
	}

	switch environment(it) {
	case envProd:
		b.adjust(0.20, "production")
	case envNonProd:
		b.adjust(-0.30, "non-production")
	}

	// A wildcard or multi-SAN certificate takes down everything it covers.
	if hosts := hostList(it); len(hosts) > 0 {
		if hasWildcard(hosts) {
			b.adjust(0.10, "wildcard certificate")
		}
		if len(hosts) >= 5 {
			b.adjust(0.05, strconv.Itoa(len(hosts))+" hosts covered")
		}
	}

	// Traffic, when anything actually reports it, beats every other guess.
	if rps, err := strconv.ParseFloat(it.Labels[source.LabelTraffic], 64); err == nil && rps > 1 {
		// log10-scaled and capped: 10 rps adds 0.1, 1k rps adds 0.3, and past
		// that "very busy" is the same answer.
		b.adjust(math.Min(0.30, math.Log10(rps)*0.10), "traffic "+trimFloat(rps)+" rps")
	}

	if it.Labels["in-use"] == "false" {
		b.adjust(-0.35, "not in use")
	}

	return clamp01(b.score), b.why(it.Kind)
}

// blastScore accumulates a blast radius and the evidence that moved it. The
// evidence is not decoration: a ranking nobody can explain gets ignored, so
// every adjustment records why it happened at the moment it happens.
type blastScore struct {
	score   float64
	reasons []string
}

func (b *blastScore) adjust(delta float64, reason string) {
	b.score += delta
	b.reasons = append(b.reasons, reason)
}

func (b *blastScore) why(kind source.Kind) string {
	why := "base " + string(kind)
	if len(b.reasons) > 0 {
		why += ", " + strings.Join(b.reasons, ", ")
	}
	return why
}

func matchOverride(it source.Item, overrides []Override) (Override, bool) {
	candidates := []string{it.Name, it.Namespace, it.Namespace + "/" + it.Name}
	for _, o := range overrides {
		for _, c := range candidates {
			if ok, err := path.Match(o.Match, c); err == nil && ok {
				return o, true
			}
		}
	}
	return Override{}, false
}

// ValidateOverrides rejects patterns path.Match cannot parse. Without this a
// malformed glob simply never matches, so the operator who pinned their payment
// path to blast radius 1.0 gets silently ranked as if they never had — the one
// failure mode an override exists to prevent.
func ValidateOverrides(overrides []Override) error {
	for i, o := range overrides {
		if o.Match == "" {
			return fmt.Errorf("override %d has no match pattern", i)
		}
		if _, err := path.Match(o.Match, "probe"); err != nil {
			return fmt.Errorf("override %d (%q): %w", i, o.Match, err)
		}
		if o.BlastRadius < 0 || o.BlastRadius > 1 {
			return fmt.Errorf("override %d (%q): blastRadius %v is outside 0..1", i, o.Match, o.BlastRadius)
		}
	}
	return nil
}

func isPublic(it source.Item) bool {
	if it.Labels[source.LabelPublic] == "true" {
		return true
	}
	class := it.Labels[source.LabelIngressClass]
	return class != "" && !isInternalClass(class)
}

func isInternalClass(class string) bool {
	c := strings.ToLower(class)
	return strings.Contains(c, "internal") || strings.Contains(c, "private") || strings.Contains(c, "intranet")
}

type env int

const (
	envUnknown env = iota
	envProd
	envNonProd
)

// Namespace naming is the only environment signal most clusters have, and it is
// right far more often than it is wrong. Non-production wins ties: mistaking
// prod for staging is an outage, the other way round is a wasted alert.
func environment(it source.Item) env {
	haystack := strings.ToLower(it.Namespace + " " + it.Name + " " + it.Labels["environment"] + " " + it.Labels["env"])
	for _, s := range []string{"staging", "sandbox", "preprod", "pre-prod", "qa", "uat", "canary", "dev", "test", "demo"} {
		if containsToken(haystack, s) {
			return envNonProd
		}
	}
	for _, s := range []string{"prod", "production", "live"} {
		if containsToken(haystack, s) {
			return envProd
		}
	}
	return envUnknown
}

// Token-ish matching so "prod" does not match inside "reproduction" and "dev"
// does not match inside "device".
func containsToken(haystack, token string) bool {
	for i := 0; i+len(token) <= len(haystack); i++ {
		if haystack[i:i+len(token)] != token {
			continue
		}
		if i > 0 && isWordChar(haystack[i-1]) {
			continue
		}
		if end := i + len(token); end < len(haystack) && isWordChar(haystack[end]) {
			continue
		}
		return true
	}
	return false
}

func isWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

func hostList(it source.Item) []string {
	raw := it.Labels[source.LabelHosts]
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func hasWildcard(hosts []string) bool {
	for _, h := range hosts {
		if strings.HasPrefix(strings.TrimSpace(h), "*.") {
			return true
		}
	}
	return false
}

func trimFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func clamp01(f float64) float64 { return math.Min(1, math.Max(0, f)) }

func round2(f float64) float64 { return math.Round(f*100) / 100 }
