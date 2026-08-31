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
