package source

import (
	"context"
	"fmt"
	"time"
)

// Kinds lists every kind an item can be, for validation and for CLI help.
var Kinds = []Kind{
	KindTLSCert,
	KindIntermediate,
	KindSecret,
	KindIAMKey,
	KindVaultLease,
	KindDomain,
}

// ManualItem is something that expires that no source can discover: a domain at
// a registrar with no RDAP, a credential rotated by hand, a code-signing
// certificate on somebody's laptop, a support contract.
//
// It carries what ranking needs rather than only a date — Kind picks the base
// blast radius, Namespace and Labels feed the same evidence every discovered
// item is weighted by, and `overrides` matches these by name like any other. A
// manual item is therefore ranked by the same rules as the rest of the estate,
// not pinned to the bottom of the list for having been typed in.
type ManualItem struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// RFC 3339, or a plain YYYY-MM-DD: a renewal date is something a person
	// writes down, and demanding a timestamp for it invites a typo.
	Expires   string            `json:"expires"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

const dateOnly = "2006-01-02"

// ExpiresAt parses the date. A day with no time means its start in UTC, which
// errs towards reporting the item as expiring sooner — the safe direction for
// something whose whole job is to warn early.
func (m ManualItem) ExpiresAt() (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, m.Expires); err == nil {
		return t, nil
	}
	t, err := time.Parse(dateOnly, m.Expires)
	if err != nil {
		return time.Time{}, fmt.Errorf("expires %q is neither YYYY-MM-DD nor RFC 3339", m.Expires)
	}
	return t, nil
}

// ValidateManual rejects entries that would otherwise load and rank wrongly.
//
// An unknown kind is the case worth catching: ranking falls back to a middling
// base for one, so `tls-cert` for `tls_cert` does not fail — it produces a
// plausible number that is wrong, which is worse than an error.
func ValidateManual(items []ManualItem) error {
	known := map[Kind]bool{}
	for _, k := range Kinds {
		known[k] = true
	}
	for i, m := range items {
		where := fmt.Sprintf("manual item %d", i)
		if m.Name != "" {
			where = fmt.Sprintf("manual item %d (%q)", i, m.Name)
		}
		if m.Name == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if m.Kind == "" {
			return fmt.Errorf("%s has no kind (one of %v)", where, Kinds)
		}
		if !known[m.Kind] {
			return fmt.Errorf("%s: unknown kind %q (want one of %v)", where, m.Kind, Kinds)
		}
		if m.Expires == "" {
			return fmt.Errorf("%s has no expires date", where)
		}
		if _, err := m.ExpiresAt(); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	}
	return nil
}

// ManualSource reports the items an operator asserted. It reads nothing and
// cannot fail: the config file was validated at load, because an item recorded
// by hand precisely because nothing can find it must never go missing later.
type ManualSource struct {
	Items []ManualItem
}

func (s *ManualSource) Name() string { return "manual" }

func (s *ManualSource) Collect(context.Context) ([]Item, error) {
	out := make([]Item, 0, len(s.Items))
	for _, m := range s.Items {
		expires, err := m.ExpiresAt()
		if err != nil {
			// Unreachable through config.Load, which validates first. Skipping
			// beats returning a zero time, which would read as "expired in
			// 1970" and shout at the top of every report.
			continue
		}
		out = append(out, Item{
			Kind:      m.Kind,
			Name:      m.Name,
			Expires:   expires,
			Source:    "manual",
			Namespace: m.Namespace,
			Labels:    m.Labels,
		})
	}
	return out, nil
}
