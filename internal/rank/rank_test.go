package rank

import (
	"strings"
	"testing"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

var now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func item(kind source.Kind, name string, days float64, labels map[string]string) source.Item {
	return source.Item{
		Kind:    kind,
		Name:    name,
		Expires: now.Add(time.Duration(days * float64(24*time.Hour))),
		Source:  "test",
		Labels:  labels,
	}
}

// The kill criterion for this whole tool: if blast-radius ranking cannot beat an
// unsorted list, stop. These are the cases where it has to beat one.
func TestRankingBeatsSortingByDate(t *testing.T) {
	items := []source.Item{
		item(source.KindTLSCert, "staging-dashboard", 20, map[string]string{"environment": "staging"}),
		item(source.KindTLSCert, "checkout", 45, map[string]string{source.LabelPublic: "true", source.LabelTraffic: "2000"}),
	}
	got := Rank(items, nil, now)

	if got[0].Item.Name != "checkout" {
		t.Fatalf("a busy public cert 45 days out must outrank a staging cert 20 days out; got %s first", got[0].Item.Name)
	}
	if got[0].BlastRadius <= got[1].BlastRadius {
		t.Errorf("blast radius did not separate the two: %v vs %v", got[0].BlastRadius, got[1].BlastRadius)
	}
}

func TestExpiredSortsFirst(t *testing.T) {
	items := []source.Item{
		item(source.KindDomain, "example.com", 200, nil),
		item(source.KindSecret, "staging/old-token", -3, map[string]string{"environment": "staging"}),
	}
	got := Rank(items, nil, now)
	if got[0].Item.Name != "staging/old-token" {
		t.Fatalf("already-expired items must lead; got %s", got[0].Item.Name)
	}
	if got[0].DaysLeft >= 0 {
		t.Errorf("expected negative DaysLeft, got %v", got[0].DaysLeft)
	}
}

func TestOverrideWins(t *testing.T) {
	items := []source.Item{
		{Kind: source.KindTLSCert, Name: "internal-thing", Namespace: "payments", Expires: now.AddDate(0, 6, 0), Source: "test"},
	}
	got := Rank(items, []Override{{Match: "payments*", BlastRadius: 1}}, now)
	if got[0].BlastRadius != 1 {
		t.Fatalf("override should pin blast radius to 1, got %v", got[0].BlastRadius)
	}
	if !strings.Contains(got[0].Why, "override") {
		t.Errorf("the explanation must name the override, got %q", got[0].Why)
	}
}

func TestResourceLabelBeatsInference(t *testing.T) {
	got := Rank([]source.Item{
		item(source.KindSecret, "dev/thing", 10, map[string]string{source.LabelBlastRadius: "0.95"}),
	}, nil, now)
	if got[0].BlastRadius != 0.95 {
		t.Fatalf("an explicit label should win over inference, got %v", got[0].BlastRadius)
	}
}

func TestInference(t *testing.T) {
	cases := []struct {
		name   string
		item   source.Item
		expect func(float64) bool
		desc   string
	}{
		{
			name:   "intermediate CA outranks a plain leaf",
			item:   item(source.KindIntermediate, "Some Issuing CA", 30, nil),
			expect: func(b float64) bool { return b >= 0.8 },
			desc:   ">= 0.8",
		},
		{
			name:   "non-production is discounted",
			item:   item(source.KindTLSCert, "app", 30, map[string]string{"environment": "staging"}),
			expect: func(b float64) bool { return b < 0.5 },
			desc:   "< 0.5",
		},
		{
			name:   "an internal ingress class is discounted",
			item:   item(source.KindTLSCert, "app", 30, map[string]string{source.LabelIngressClass: "nginx-internal"}),
			expect: func(b float64) bool { return b < 0.5 },
			desc:   "< 0.5",
		},
		{
			name:   "an unused ACM certificate is nearly free to lose",
			item:   item(source.KindTLSCert, "old.example.com", 5, map[string]string{"in-use": "false"}),
			expect: func(b float64) bool { return b <= 0.2 },
			desc:   "<= 0.2",
		},
		{
			name:   "a wildcard covering many hosts is worse than one host",
			item:   item(source.KindTLSCert, "wild", 30, map[string]string{source.LabelHosts: "*.example.com,a.example.com,b.example.com,c.example.com,d.example.com"}),
			expect: func(b float64) bool { return b > 0.6 },
			desc:   "> 0.6",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Rank([]source.Item{tc.item}, nil, now)[0]
			if !tc.expect(got.BlastRadius) {
				t.Errorf("blast radius %v, want %s (why: %s)", got.BlastRadius, tc.desc, got.Why)
			}
			if got.Why == "" {
				t.Error("every score must explain itself")
			}
		})
	}
}

// "dev" inside "device" and "prod" inside "reproduction" are the classic ways a
// substring match mislabels an environment.
func TestEnvironmentMatchesTokensNotSubstrings(t *testing.T) {
	if environment(source.Item{Name: "device-registry", Namespace: "iot"}) != envUnknown {
		t.Error("device-registry must not read as dev")
	}
	if environment(source.Item{Name: "reproduction-service"}) != envUnknown {
		t.Error("reproduction-service must not read as prod")
	}
	if environment(source.Item{Namespace: "prod"}) != envProd {
		t.Error("the prod namespace must read as production")
	}
	if environment(source.Item{Namespace: "team-staging"}) != envNonProd {
		t.Error("team-staging must read as non-production")
	}
}

func TestUrgencyRamp(t *testing.T) {
	if u := urgency(-1); u != 1 {
		t.Errorf("expired urgency = %v, want 1", u)
	}
	if u := urgency(1000); u != 0 {
		t.Errorf("far-future urgency = %v, want 0", u)
	}
	if a, b := urgency(10), urgency(60); a <= b {
		t.Errorf("urgency must increase as expiry approaches: %v vs %v", a, b)
	}
}

// gandalf finding: path.Match errors were swallowed, so a malformed glob simply
// never matched — the override failed by quietly not applying.
func TestValidateOverridesRejectsPatternsThatCouldNeverMatch(t *testing.T) {
	if err := ValidateOverrides([]Override{{Match: "payments/*", BlastRadius: 1}}); err != nil {
		t.Fatalf("a valid override was rejected: %v", err)
	}
	for _, bad := range []Override{
		{Match: "[unclosed", BlastRadius: 1},
		{Match: "", BlastRadius: 1},
		{Match: "ok", BlastRadius: 2},
		{Match: "ok", BlastRadius: -1},
	} {
		if err := ValidateOverrides([]Override{bad}); err == nil {
			t.Errorf("override %+v should have been rejected at load", bad)
		}
	}
}

// The behaviour that made the missing validation dangerous.
func TestAMalformedGlobSilentlyMatchesNothing(t *testing.T) {
	got := Rank([]source.Item{item(source.KindTLSCert, "payments-api", 30, nil)},
		[]Override{{Match: "[unclosed", BlastRadius: 1}}, now)
	if got[0].BlastRadius == 1 {
		t.Fatal("a malformed pattern must not match")
	}
	if err := ValidateOverrides([]Override{{Match: "[unclosed"}}); err == nil {
		t.Error("...which is exactly why it has to be rejected before it gets here")
	}
}

// Equal priority is common — same kind, same labels, different dates — and an
// unstable answer there means the top of the report reshuffles between runs.
func TestTiesBreakOnTheSoonerExpiry(t *testing.T) {
	items := []source.Item{
		item(source.KindTLSCert, "later", 60, nil),
		item(source.KindTLSCert, "sooner", 60, nil),
	}
	// Same kind and no labels, so priority differs only through daysLeft; force
	// an exact tie by giving both the same expiry.
	items[1].Expires = items[0].Expires
	got := Rank(items, nil, now)
	if got[0].Priority != got[1].Priority {
		t.Fatalf("test setup: expected a priority tie, got %v vs %v", got[0].Priority, got[1].Priority)
	}
	if got[0].Item.Name != "later" {
		t.Errorf("a tie must keep input order, got %s first", got[0].Item.Name)
	}

	items[1].Expires = now.Add(24 * time.Hour) // now genuinely sooner
	got = Rank(items, nil, now)
	if got[0].Item.Name != "sooner" {
		t.Errorf("the sooner expiry must lead, got %s", got[0].Item.Name)
	}
}

// An unrecognised kind, or one with nothing to infer from, still has to be
// ranked: dropping it or scoring it 0 hides it under everything else.
func TestUnknownKindStillGetsRanked(t *testing.T) {
	got := Rank([]source.Item{item(source.Kind("something-new"), "mystery", 10, nil)}, nil, now)
	if len(got) != 1 {
		t.Fatalf("want 1 scored item, got %d", len(got))
	}
	if got[0].BlastRadius <= 0 || got[0].BlastRadius > 1 {
		t.Errorf("unknown kind needs a usable default blast radius, got %v", got[0].BlastRadius)
	}
	if got[0].Why == "" {
		t.Error("every score has to explain itself, even the fallback one")
	}
}

func TestScoresStayInRangeAtBothExtremes(t *testing.T) {
	items := []source.Item{
		// Everything that adds: public, prod, wildcard, many hosts, huge traffic.
		item(source.KindDomain, "prod-checkout", -30, map[string]string{
			source.LabelPublic:  "true",
			source.LabelTraffic: "100000",
			source.LabelHosts:   "*.a.com,b.com,c.com,d.com,e.com",
			"environment":       "production",
		}),
		// Everything that subtracts.
		item(source.KindVaultLease, "dev-unused", 400, map[string]string{
			source.LabelIngressClass: "nginx-internal",
			"environment":            "dev",
			"in-use":                 "false",
		}),
	}
	for _, s := range Rank(items, nil, now) {
		if s.BlastRadius < 0 || s.BlastRadius > 1 {
			t.Errorf("%s: blast radius %v escaped 0..1", s.Item.Name, s.BlastRadius)
		}
		if s.Priority < 0 || s.Priority > 1 {
			t.Errorf("%s: priority %v escaped 0..1", s.Item.Name, s.Priority)
		}
	}
}
