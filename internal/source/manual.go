package source

import (
	"context"
	"fmt"
	"maps"
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
	KindTrustAnchor,
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
	//
	// Empty for a credential that cannot expire — see Created.
	Expires string `json:"expires,omitempty"`
	// Created is the day an unexpiring credential was issued, and with
	// MaxKeyAgeDays it stands in for Expires.
	//
	// Plenty of things have no expiry to record: an API token issued without
	// one, an ingest key whose store has no expiry column at all. Demanding a
	// date for those leaves an operator two options, omit the credential or
	// invent a date, and the invented one is worse — it renders as a confident
	// number nobody chose. This is the third option, and it is the one the
	// rotation and Cloudflare sources already take for the same problem.
	Created string `json:"created,omitempty"`
	// MaxKeyAgeDays is the rotation policy applied to Created. No default, for
	// the reason rotation.go gives: a deadline nobody chose is not a policy.
	// The operator states one here or states a date in Expires; there is no
	// third answer this tool can invent for them.
	MaxKeyAgeDays int               `json:"maxKeyAgeDays,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

const dateOnly = "2006-01-02"

// ExpiresAt parses the date. A day with no time means its start in UTC, which
// errs towards reporting the item as expiring sooner — the safe direction for
// something whose whole job is to warn early.
func (m ManualItem) ExpiresAt() (time.Time, error) {
	return manualDate("expires", m.Expires)
}

func manualDate(field, s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(dateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q is neither YYYY-MM-DD nor RFC 3339", field, s)
	}
	return t, nil
}

// deadline is the date this item is reported against, with the labels that say
// where it came from: the one the operator wrote, or — for a credential that
// cannot expire — its issue date plus the rotation policy they chose, marked
// as policy so it can never be read as a date somebody issued.
func (m ManualItem) deadline() (time.Time, map[string]string, error) {
	labels := maps.Clone(m.Labels)
	if m.Expires != "" {
		t, err := m.ExpiresAt()
		return t, labels, err
	}
	created, err := manualDate("created", m.Created)
	if err != nil {
		return time.Time{}, nil, err
	}
	t, labels := policyDeadline(labels, created, m.MaxKeyAgeDays)
	return t, labels, nil
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
		if m.Expires == "" && m.MaxKeyAgeDays <= 0 {
			return fmt.Errorf("%s has no expires date — if it cannot expire, give "+
				"created and maxKeyAgeDays instead of a date nobody chose", where)
		}
		if m.Expires != "" && (m.Created != "" || m.MaxKeyAgeDays > 0) {
			// Both would mean two deadlines, and the report can only show one.
			// Which one it showed would be an implementation detail.
			return fmt.Errorf("%s has both expires and a rotation policy: keep expires for "+
				"something with a real deadline, created and maxKeyAgeDays for something without", where)
		}
		if m.Expires == "" && m.Created == "" {
			return fmt.Errorf("%s: maxKeyAgeDays needs created — the policy runs "+
				"from the day the credential was issued", where)
		}
		if _, _, err := m.deadline(); err != nil {
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

// Name identifies this source in an item's Source field and in -only.
func (s *ManualSource) Name() string { return "manual" }

// Collect reads the items the config lists by hand, for things no API can enumerate.
//
// Read-only, like every source: expiry-radar never needs write access.
func (s *ManualSource) Collect(context.Context) ([]Item, error) {
	out := make([]Item, 0, len(s.Items))
	for _, m := range s.Items {
		expires, labels, err := m.deadline()
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
			Labels:    labels,
		})
	}
	return out, nil
}
