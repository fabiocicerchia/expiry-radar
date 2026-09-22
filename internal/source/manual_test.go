package source

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestManualItemAcceptsADateAPersonWouldWrite(t *testing.T) {
	for _, tc := range []struct {
		name, expires string
		want          time.Time
	}{
		{"plain date", "2027-03-01", time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"rfc 3339", "2027-03-01T15:04:05Z", time.Date(2027, 3, 1, 15, 4, 5, 0, time.UTC)},
		{"rfc 3339 with offset", "2027-03-01T15:04:05+02:00", time.Date(2027, 3, 1, 13, 4, 5, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ManualItem{Expires: tc.expires}.ExpiresAt()
			if err != nil {
				t.Fatalf("ExpiresAt(%q): %v", tc.expires, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("ExpiresAt(%q) = %v, want %v", tc.expires, got.UTC(), tc.want)
			}
		})
	}
}

// Every rejection here is an entry that would otherwise load and be ranked on a
// number nobody chose — worse than an error, because it looks like an answer.
func TestValidateManualRejectsEntriesThatWouldRankWrongly(t *testing.T) {
	for _, tc := range []struct {
		name string
		item ManualItem
		want string
	}{
		{"no name", ManualItem{Kind: KindDomain, Expires: "2027-03-01"}, "no name"},
		{"no kind", ManualItem{Name: "a", Expires: "2027-03-01"}, "no kind"},
		{
			// The case that matters: ranking falls back to a middling base for
			// an unknown kind, so this would produce a plausible wrong number.
			"kind with a typo",
			ManualItem{Name: "a", Kind: "tls-cert", Expires: "2027-03-01"},
			`unknown kind "tls-cert"`,
		},
		{"no date", ManualItem{Name: "a", Kind: KindDomain}, "no expires date"},
		{"unparsable date", ManualItem{Name: "a", Kind: KindDomain, Expires: "1 March 2027"}, "neither"},
		{"american date", ManualItem{Name: "a", Kind: KindDomain, Expires: "03/01/2027"}, "neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateManual([]ManualItem{tc.item})
			if err == nil {
				t.Fatalf("expected %s to be rejected, it validated", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should mention %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateManualNamesTheOffendingEntry(t *testing.T) {
	err := ValidateManual([]ManualItem{
		{Name: "fine.example.com", Kind: KindDomain, Expires: "2027-03-01"},
		{Name: "broken.example.com", Kind: KindDomain, Expires: "soon"},
	})
	if err == nil {
		t.Fatal("expected the second entry to be rejected")
	}
	// A config with thirty entries is no use if the error says only "invalid".
	if !strings.Contains(err.Error(), "broken.example.com") || !strings.Contains(err.Error(), "1") {
		t.Errorf("error should name the entry and its index, got %v", err)
	}
}

func TestValidateManualAcceptsEveryKindTheToolReports(t *testing.T) {
	for _, kind := range Kinds {
		if err := ValidateManual([]ManualItem{{Name: "a", Kind: kind, Expires: "2027-03-01"}}); err != nil {
			t.Errorf("kind %q should be valid: %v", kind, err)
		}
	}
}

func TestManualSourceReportsWhatWasRecorded(t *testing.T) {
	src := &ManualSource{Items: []ManualItem{{
		Name:      "acme-corp.co.uk",
		Kind:      KindDomain,
		Expires:   "2027-03-01",
		Namespace: "corp",
		Labels:    map[string]string{LabelPublic: "true"},
	}}}
	if src.Name() != "manual" {
		t.Errorf("Name() = %q, want manual", src.Name())
	}

	items, err := src.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	got := items[0]
	if got.Name != "acme-corp.co.uk" || got.Kind != KindDomain || got.Namespace != "corp" {
		t.Errorf("item did not round-trip: %+v", got)
	}
	if got.Source != "manual" {
		t.Errorf("Source = %q, want manual — the report has to say where this came from", got.Source)
	}
	// The labels are the whole reason a manual item can be ranked rather than
	// parked at the bottom of the list.
	if got.Labels[LabelPublic] != "true" {
		t.Errorf("labels were dropped: %+v", got.Labels)
	}
	if !got.Expires.Equal(time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Expires = %v", got.Expires)
	}
}

func TestManualSourceIsEmptyWithoutItems(t *testing.T) {
	items, err := (&ManualSource{}).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("got %d items, want none", len(items))
	}
}

// A credential that cannot expire is the case the operator used to solve by
// inventing a date, which renders as a confident green number nobody chose.
func TestManualSourceDatesAnUnexpiringCredentialFromThePolicy(t *testing.T) {
	src := &ManualSource{Items: []ManualItem{{
		Name:          "stillvalid-ingest",
		Kind:          KindSecret,
		Created:       "2026-01-01",
		MaxKeyAgeDays: 90,
		Labels:        map[string]string{LabelPublic: "true"},
	}}}
	items, err := src.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	got := items[0]
	if want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC); !got.Expires.Equal(want) {
		t.Errorf("Expires = %v, want %v (created + 90 days)", got.Expires, want)
	}
	// Without these the date is indistinguishable from one an issuer stated,
	// which is the fiction this whole shape exists to avoid.
	if got.Labels["deadline"] != "rotation policy" || got.Labels["policy.days"] != "90" ||
		got.Labels["created"] != "2026-01-01T00:00:00Z" {
		t.Errorf("a synthesised deadline must say so: %+v", got.Labels)
	}
	if got.Labels[LabelPublic] != "true" {
		t.Errorf("the operator's own labels were dropped: %+v", got.Labels)
	}
	// The config's map must survive collection unedited: it is shared with
	// overrides and with every later run in a long-lived process.
	if _, ok := src.Items[0].Labels["deadline"]; ok {
		t.Errorf("Collect wrote back into the config's labels: %+v", src.Items[0].Labels)
	}
}

func TestValidateManualRejectsHalfARotationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		item ManualItem
		want string
	}{
		{
			"policy without a created date",
			ManualItem{Name: "a", Kind: KindSecret, MaxKeyAgeDays: 90},
			"needs created",
		},
		{
			// Two deadlines, and which one won would be an implementation detail.
			"a date and a policy",
			ManualItem{Name: "a", Kind: KindSecret, Expires: "2027-03-01", Created: "2026-01-01", MaxKeyAgeDays: 90},
			"both expires and a rotation policy",
		},
		{
			// No default, for the reason rotation.go gives: a deadline nobody
			// chose is not a policy.
			"neither",
			ManualItem{Name: "a", Kind: KindSecret, Created: "2026-01-01"},
			"no expires date",
		},
		{"unparsable created", ManualItem{Name: "a", Kind: KindSecret, Created: "last spring", MaxKeyAgeDays: 90}, "neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateManual([]ManualItem{tc.item})
			if err == nil {
				t.Fatalf("expected %s to be rejected, it validated", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should mention %q, got %v", tc.want, err)
			}
		})
	}
}
